package frontiercheckpoint

import (
	"bytes"
	"context"
	"fmt"
	"math"

	bolt "go.etcd.io/bbolt"
)

const pendingPageBudgetBatchSize = 256

func (checkpoint *FrontierCheckpoint) TrimPendingPages(
	ctx context.Context,
	provenance []byte,
	keep uint64,
) (uint64, error) {
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		return 0, err
	}
	removed := uint64(0)
	for {
		batchRemoved, complete, err := checkpoint.trimPendingPageBudgetBatch(
			ctx,
			provenance,
			prefix,
			keep,
		)
		removed += batchRemoved
		if err != nil || complete {
			return removed, err
		}
	}
}

func (checkpoint *FrontierCheckpoint) trimPendingPageBudgetBatch(
	ctx context.Context,
	provenance []byte,
	prefix []byte,
	keep uint64,
) (uint64, bool, error) {
	trim := pendingPageBudgetTrim{
		provenance: provenance,
		prefix:     prefix,
		keep:       keep,
	}
	err := checkpoint.boundedWriteTransaction(ctx, trim.apply)

	return trim.removed, trim.complete, err
}

func newestPendingPageURLs(bucket *bolt.Bucket, prefix []byte, limit uint64) ([]string, error) {
	if limit == 0 {
		return nil, nil
	}
	cursor := bucket.Cursor()
	maximumKey := sequenceRowKey(prefix, math.MaxUint64)
	key, encoded := cursor.Seek(maximumKey)
	if key == nil || !bytes.Equal(key, maximumKey) {
		key, encoded = cursor.Prev()
	}
	pageURLs := make([]string, 0, pendingPageBudgetBatchSize)
	remaining := limit
	for key != nil && bytes.HasPrefix(key, prefix) && remaining > 0 {
		if len(key) != len(prefix)+8 {
			return nil, fmt.Errorf("%w: invalid outstanding page key", ErrCorruptCheckpoint)
		}
		var page Page
		if err := decodeRow("page", encoded, &page); err != nil {
			return nil, err
		}
		if err := validatePages([]Page{page}); err != nil {
			return nil, fmt.Errorf("%w: persisted page is invalid", ErrCorruptCheckpoint)
		}
		pageURLs = append(pageURLs, page.URL)
		remaining--
		key, encoded = cursor.Prev()
	}

	return pageURLs, nil
}
