package documentstore

import (
	"bytes"
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const storedDocumentPageSize = 16

type StoredDocumentPageScanner interface {
	ScanStoredDocumentPages(
		ctx context.Context,
		visit func(Document) (bool, error),
	) error
}

type storedDocumentPartition uint8

const (
	legacyDocumentPartition storedDocumentPartition = iota
	orderedDocumentPartition
)

type storedDocumentPartitionBoundary struct {
	partition storedDocumentPartition
	bucket    vault.Name
	lastKey   vault.Key
}

type storedDocumentRawPage struct {
	entries  []vault.BucketPageEntry
	cursor   vault.Key
	complete bool
}

type storedDocumentPageEntry struct {
	key      vault.Key
	document Document
}

func (d documentVault) ScanStoredDocumentPages(
	ctx context.Context,
	visit func(Document) (bool, error),
) error {
	release, err := d.enterStoredDocumentScan(ctx)
	if err != nil {
		return err
	}
	defer release()

	if err := d.scanStoredDocumentPages(ctx, storedDocumentPageSize, visit); err != nil {
		return fmt.Errorf("scan stored document pages: %w", err)
	}

	return nil
}

func (d documentVault) scanStoredDocumentPages(
	ctx context.Context,
	pageSize int,
	visit func(Document) (bool, error),
) error {
	if pageSize < 1 {
		return fmt.Errorf("page size must be positive: %d", pageSize)
	}
	boundaries, err := d.captureStoredDocumentPartitionBoundaries(ctx)
	if err != nil {
		return err
	}
	for _, boundary := range boundaries {
		if boundary.lastKey == nil {
			continue
		}
		if err := d.scanStoredDocumentPartition(
			ctx,
			boundary,
			pageSize,
			visit,
		); err != nil {
			return err
		}
	}

	return nil
}

func (d documentVault) captureStoredDocumentPartitionBoundaries(
	ctx context.Context,
) ([]storedDocumentPartitionBoundary, error) {
	releaseBoundary, err := d.enterStoredDocumentScanBoundary(ctx)
	if err != nil {
		return nil, err
	}
	defer releaseBoundary()
	boundaries := []storedDocumentPartitionBoundary{
		{partition: legacyDocumentPartition, bucket: bucketName},
		{partition: orderedDocumentPartition, bucket: orderedDocumentBucketName},
	}
	err = d.vault.View(vault.BackgroundRead(ctx), func(tx *vault.Txn) error {
		for index := range boundaries {
			lastKey, err := tx.ReadBucketLastKey(boundaries[index].bucket)
			if err != nil {
				return fmt.Errorf(
					"read %s document high key: %w",
					boundaries[index].bucket,
					err,
				)
			}
			boundaries[index].lastKey = lastKey
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("capture stored document boundaries: %w", err)
	}

	return boundaries, nil
}

func (d documentVault) scanStoredDocumentPartition(
	ctx context.Context,
	boundary storedDocumentPartitionBoundary,
	pageSize int,
	visit func(Document) (bool, error),
) error {
	var after vault.Key
	for {
		page, err := d.readStoredDocumentRawPage(ctx, boundary, after, pageSize)
		if err != nil {
			return err
		}
		authoritative, err := d.authoritativeStoredDocumentRawPage(
			ctx,
			boundary.partition,
			page.entries,
		)
		if err != nil {
			return err
		}
		entries, err := decodeStoredDocumentRawPage(ctx, authoritative)
		if err != nil {
			return err
		}
		documents, err := visibleStoredDocumentPage(
			ctx,
			boundary.partition,
			entries,
		)
		if err != nil {
			return err
		}
		keep, err := visitStoredDocumentPage(ctx, documents, visit)
		if err != nil || !keep {
			return err
		}
		if page.complete {
			return nil
		}
		after = page.cursor
	}
}

func (d documentVault) readStoredDocumentRawPage(
	ctx context.Context,
	boundary storedDocumentPartitionBoundary,
	after vault.Key,
	pageSize int,
) (storedDocumentRawPage, error) {
	var page vault.BucketPage
	err := d.vault.View(vault.BackgroundRead(ctx), func(tx *vault.Txn) error {
		read, err := tx.ReadBucketPage(boundary.bucket, after, pageSize)
		page = read
		if err != nil {
			return fmt.Errorf("read bucket page: %w", err)
		}

		return nil
	})
	if err != nil {
		return storedDocumentRawPage{}, fmt.Errorf(
			"read %s document page: %w",
			boundary.bucket,
			err,
		)
	}
	selected := storedDocumentRawPage{
		entries: make([]vault.BucketPageEntry, 0, len(page.Entries)),
		cursor:  after,
	}
	for _, entry := range page.Entries {
		keyOrder := bytes.Compare(entry.Key, boundary.lastKey)
		if keyOrder > 0 {
			selected.complete = true

			return selected, nil
		}
		selected.entries = append(selected.entries, entry)
		selected.cursor = entry.Key
		if keyOrder == 0 {
			selected.complete = true

			return selected, nil
		}
	}
	selected.complete = !page.More

	return selected, nil
}

func decodeStoredDocumentRawPage(
	ctx context.Context,
	entries []vault.BucketPageEntry,
) ([]storedDocumentPageEntry, error) {
	documents := make([]storedDocumentPageEntry, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("context: %w", err)
		}
		document, err := (documentCodec{}).Decode(entry.Value)
		if err != nil {
			continue
		}
		documents = append(documents, storedDocumentPageEntry{
			key:      entry.Key,
			document: document,
		})
	}

	return documents, nil
}

func (d documentVault) authoritativeStoredDocumentRawPage(
	ctx context.Context,
	partition storedDocumentPartition,
	entries []vault.BucketPageEntry,
) ([]vault.BucketPageEntry, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	authoritative := make([]vault.BucketPageEntry, 0, len(entries))
	err := d.vault.View(vault.BackgroundRead(ctx), func(tx *vault.Txn) error {
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("context: %w", err)
			}
			visible, err := d.storedDocumentPageEntryAuthority(
				tx,
				partition,
				entry,
			)
			if err != nil {
				return err
			}
			if visible {
				authoritative = append(authoritative, entry)
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("validate stored document page: %w", err)
	}

	return authoritative, nil
}

func (d documentVault) storedDocumentPageEntryAuthority(
	tx *vault.Txn,
	partition storedDocumentPartition,
	entry vault.BucketPageEntry,
) (bool, error) {
	if partition == legacyDocumentPartition {
		_, located, err := d.documentLocations.Get(tx, entry.Key)
		if err != nil {
			return false, fmt.Errorf("read document page location: %w", err)
		}

		return !located, nil
	}
	admission, normalizedURL, decodeErr := decodeOrderedDocumentKey(entry.Key)
	if decodeErr == nil {
		locatedAdmission, located, err := d.documentLocations.Get(
			tx,
			vault.Key(normalizedURL),
		)
		if err != nil {
			return false, fmt.Errorf("read document page location: %w", err)
		}

		return located && locatedAdmission == admission, nil
	}

	return false, nil
}

func visibleStoredDocumentPage(
	ctx context.Context,
	partition storedDocumentPartition,
	entries []storedDocumentPageEntry,
) ([]Document, error) {
	documents := make([]Document, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("context: %w", err)
		}
		normalizedURL := string(entry.key)
		if partition == orderedDocumentPartition {
			var err error
			_, normalizedURL, err = decodeOrderedDocumentKey(entry.key)
			if err != nil {
				continue
			}
		}
		if entry.document.NormalizedURL != normalizedURL {
			continue
		}
		documents = append(documents, entry.document)
	}

	return documents, nil
}

func visitStoredDocumentPage(
	ctx context.Context,
	documents []Document,
	visit func(Document) (bool, error),
) (bool, error) {
	for _, document := range documents {
		if err := ctx.Err(); err != nil {
			return false, fmt.Errorf("context: %w", err)
		}
		keep, err := visit(document)
		if err != nil || !keep {
			return keep, err
		}
	}

	return true, nil
}

var _ StoredDocumentPageScanner = documentVault{}
