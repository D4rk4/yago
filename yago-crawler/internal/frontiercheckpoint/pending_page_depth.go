package frontiercheckpoint

import (
	"bytes"
	"context"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

func (checkpoint *FrontierCheckpoint) DiscardPendingPagesBeyondDepth(
	ctx context.Context,
	provenance []byte,
	maximumDepth int,
) (uint64, error) {
	if maximumDepth < 0 {
		return 0, fmt.Errorf("maximum pending page depth must not be negative")
	}
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		return 0, err
	}
	discarded := uint64(0)
	for {
		batchDiscarded, complete, err := checkpoint.discardPendingPageDepthBatch(
			ctx,
			provenance,
			prefix,
			maximumDepth,
		)
		discarded += batchDiscarded
		if err != nil || complete {
			return discarded, err
		}
	}
}

func (checkpoint *FrontierCheckpoint) discardPendingPageDepthBatch(
	ctx context.Context,
	provenance []byte,
	prefix []byte,
	maximumDepth int,
) (uint64, bool, error) {
	discard := pendingPageDepthDiscard{
		provenance:   provenance,
		prefix:       prefix,
		maximumDepth: maximumDepth,
	}
	err := checkpoint.boundedWriteTransaction(ctx, discard.apply)

	return discard.discarded, discard.complete, err
}

func pendingPageURLsBeyondDepth(
	bucket *bolt.Bucket,
	prefix []byte,
	maximumDepth int,
) ([]string, bool, error) {
	pageURLs := make([]string, 0, pendingPageBudgetBatchSize)
	cursor := bucket.Cursor()
	for key, encoded := cursor.Seek(prefix); key != nil && bytes.HasPrefix(key, prefix); key, encoded = cursor.Next() {
		if len(key) != len(prefix)+8 {
			return nil, false, fmt.Errorf("%w: invalid outstanding page key", ErrCorruptCheckpoint)
		}
		var page Page
		if err := decodeRow("page", encoded, &page); err != nil {
			return nil, false, err
		}
		if err := validatePages([]Page{page}); err != nil {
			return nil, false, fmt.Errorf("%w: persisted page is invalid", ErrCorruptCheckpoint)
		}
		if page.Depth <= maximumDepth {
			continue
		}
		pageURLs = append(pageURLs, page.URL)
		if len(pageURLs) == pendingPageBudgetBatchSize {
			return pageURLs, false, nil
		}
	}

	return pageURLs, true, nil
}
