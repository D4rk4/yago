package frontiercheckpoint

import (
	"fmt"

	bolt "go.etcd.io/bbolt"
)

type pendingPageDepthDiscard struct {
	provenance   []byte
	prefix       []byte
	maximumDepth int
	discarded    uint64
	complete     bool
}

func (discard *pendingPageDepthDiscard) apply(transaction *bolt.Tx) error {
	record, err := requiredRunRecord(transaction, discard.provenance)
	if err != nil {
		return err
	}
	if err := validatePendingPageBudget(record); err != nil {
		return err
	}
	buckets, err := loadCheckpointBuckets(transaction)
	if err != nil {
		return err
	}
	pageURLs, complete, err := pendingPageURLsBeyondDepth(
		buckets.pages,
		discard.prefix,
		discard.maximumDepth,
	)
	if err != nil {
		return err
	}
	removed := uint64(0)
	if err := removePendingPageBudgetRows(
		buckets,
		discard.prefix,
		pageURLs,
		&removed,
	); err != nil {
		return err
	}
	if removed != uint64(len(pageURLs)) || removed > record.Pending {
		return fmt.Errorf("%w: pending page depth removal is incomplete", ErrCorruptCheckpoint)
	}
	record.Pending -= removed
	record.BudgetDiscardedPages += removed
	markCompletion(&record, buckets.pages, discard.prefix)
	discard.discarded += removed
	discard.complete = complete

	return writeRunRecord(transaction, discard.provenance, record)
}
