package documentstore

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const MaximumStoredDocumentBatchSize = 100

const (
	storedDocumentBatchContinuationVersion = 1
	maximumStoredDocumentContinuationBytes = 16 << 10
)

type StoredDocumentBatchReader interface {
	ReadStoredDocumentBatch(
		ctx context.Context,
		continuation string,
		limit int,
	) (StoredDocumentBatch, error)
}

type StoredDocumentBatch struct {
	Documents    []Document
	Examined     int
	Continuation string
	Complete     bool
}

type storedDocumentBatchPosition struct {
	Version    uint8
	Partition  storedDocumentPartition
	After      vault.Key
	LegacyEnd  vault.Key
	OrderedEnd vault.Key
}

func (d documentVault) ReadStoredDocumentBatch(
	ctx context.Context,
	continuation string,
	limit int,
) (StoredDocumentBatch, error) {
	if limit < 1 || limit > MaximumStoredDocumentBatchSize {
		return StoredDocumentBatch{}, fmt.Errorf(
			"stored document batch limit must be between 1 and %d",
			MaximumStoredDocumentBatchSize,
		)
	}
	release, err := d.enterStoredDocumentScan(ctx)
	if err != nil {
		return StoredDocumentBatch{}, err
	}
	defer release()

	position, err := d.storedDocumentBatchPosition(ctx, continuation)
	if err != nil {
		return StoredDocumentBatch{}, err
	}
	batch, err := d.readStoredDocumentBatch(ctx, position, limit)
	if err != nil {
		return StoredDocumentBatch{}, fmt.Errorf("read stored document batch: %w", err)
	}

	return batch, nil
}

func (d documentVault) storedDocumentBatchPosition(
	ctx context.Context,
	continuation string,
) (storedDocumentBatchPosition, error) {
	if continuation != "" {
		return parseStoredDocumentBatchPosition(continuation)
	}
	boundaries, err := d.captureStoredDocumentPartitionBoundaries(ctx)
	if err != nil {
		return storedDocumentBatchPosition{}, err
	}
	position := storedDocumentBatchPosition{
		Version:   storedDocumentBatchContinuationVersion,
		Partition: legacyDocumentPartition,
	}
	for _, boundary := range boundaries {
		switch boundary.partition {
		case legacyDocumentPartition:
			position.LegacyEnd = append(vault.Key(nil), boundary.lastKey...)
		case orderedDocumentPartition:
			position.OrderedEnd = append(vault.Key(nil), boundary.lastKey...)
		}
	}

	return position, nil
}

func (d documentVault) readStoredDocumentBatch(
	ctx context.Context,
	position storedDocumentBatchPosition,
	limit int,
) (StoredDocumentBatch, error) {
	batch := StoredDocumentBatch{Documents: make([]Document, 0, limit)}
	for {
		position = advanceEmptyStoredDocumentPartitions(position)
		if position.Partition > orderedDocumentPartition {
			batch.Complete = true

			return batch, nil
		}
		if batch.Examined >= limit {
			batch.Continuation = formatStoredDocumentBatchPosition(position)

			return batch, nil
		}

		boundary := position.storedDocumentPartitionBoundary()
		page, err := d.readStoredDocumentRawPage(
			ctx,
			boundary,
			position.After,
			limit-batch.Examined,
		)
		if err != nil {
			return StoredDocumentBatch{}, err
		}
		batch.Examined += len(page.entries)
		authoritative, err := d.authoritativeStoredDocumentRawPage(
			ctx,
			position.Partition,
			page.entries,
		)
		if err != nil {
			return StoredDocumentBatch{}, err
		}
		entries, err := decodeStoredDocumentRawPage(ctx, authoritative)
		if err != nil {
			return StoredDocumentBatch{}, err
		}
		documents, err := visibleStoredDocumentPage(ctx, position.Partition, entries)
		if err != nil {
			return StoredDocumentBatch{}, err
		}
		batch.Documents = append(batch.Documents, documents...)
		position.After = append(vault.Key(nil), page.cursor...)
		if page.complete {
			position.Partition++
			position.After = nil

			continue
		}
		batch.Continuation = formatStoredDocumentBatchPosition(position)

		return batch, nil
	}
}

func advanceEmptyStoredDocumentPartitions(
	position storedDocumentBatchPosition,
) storedDocumentBatchPosition {
	for position.Partition <= orderedDocumentPartition &&
		len(position.partitionEnd()) == 0 {
		position.Partition++
		position.After = nil
	}

	return position
}

func (position storedDocumentBatchPosition) partitionEnd() vault.Key {
	if position.Partition == legacyDocumentPartition {
		return position.LegacyEnd
	}

	return position.OrderedEnd
}

func (position storedDocumentBatchPosition) storedDocumentPartitionBoundary() storedDocumentPartitionBoundary {
	bucket := bucketName
	if position.Partition == orderedDocumentPartition {
		bucket = orderedDocumentBucketName
	}

	return storedDocumentPartitionBoundary{
		partition: position.Partition,
		bucket:    bucket,
		lastKey:   position.partitionEnd(),
	}
}

func formatStoredDocumentBatchPosition(
	position storedDocumentBatchPosition,
) string {
	raw := append([]byte(nil), `{"Version":`...)
	raw = strconv.AppendUint(raw, uint64(position.Version), 10)
	raw = append(raw, `,"Partition":`...)
	raw = strconv.AppendUint(raw, uint64(position.Partition), 10)
	raw = append(raw, `,"After":"`...)
	raw = base64.StdEncoding.AppendEncode(raw, position.After)
	raw = append(raw, `","LegacyEnd":"`...)
	raw = base64.StdEncoding.AppendEncode(raw, position.LegacyEnd)
	raw = append(raw, `","OrderedEnd":"`...)
	raw = base64.StdEncoding.AppendEncode(raw, position.OrderedEnd)
	raw = append(raw, `"}`...)

	return base64.RawURLEncoding.EncodeToString(raw)
}

func parseStoredDocumentBatchPosition(
	continuation string,
) (storedDocumentBatchPosition, error) {
	if len(continuation) > maximumStoredDocumentContinuationBytes {
		return storedDocumentBatchPosition{}, fmt.Errorf(
			"stored document continuation is too large",
		)
	}
	raw, err := base64.RawURLEncoding.DecodeString(continuation)
	if err != nil {
		return storedDocumentBatchPosition{}, fmt.Errorf(
			"decode stored document continuation: %w",
			err,
		)
	}
	var position storedDocumentBatchPosition
	if err := json.Unmarshal(raw, &position); err != nil {
		return storedDocumentBatchPosition{}, fmt.Errorf(
			"parse stored document continuation: %w",
			err,
		)
	}
	if position.Version != storedDocumentBatchContinuationVersion {
		return storedDocumentBatchPosition{}, fmt.Errorf(
			"unsupported stored document continuation version %d",
			position.Version,
		)
	}
	if position.Partition > orderedDocumentPartition {
		return storedDocumentBatchPosition{}, fmt.Errorf(
			"invalid stored document continuation partition %d",
			position.Partition,
		)
	}
	if err := validateStoredDocumentBatchPosition(position); err != nil {
		return storedDocumentBatchPosition{}, err
	}

	return position, nil
}

func validateStoredDocumentBatchPosition(position storedDocumentBatchPosition) error {
	maximumKeyBytes := yagomodel.MaximumURLIdentityBytes + orderedDocumentAdmissionSize
	for name, key := range map[string]vault.Key{
		"after":       position.After,
		"legacy end":  position.LegacyEnd,
		"ordered end": position.OrderedEnd,
	} {
		if len(key) > maximumKeyBytes {
			return fmt.Errorf("stored document continuation %s is too large", name)
		}
	}
	end := position.partitionEnd()
	if len(position.After) > 0 &&
		(len(end) == 0 || bytes.Compare(position.After, end) > 0) {
		return fmt.Errorf("stored document continuation is past its boundary")
	}

	return nil
}

var _ StoredDocumentBatchReader = documentVault{}
