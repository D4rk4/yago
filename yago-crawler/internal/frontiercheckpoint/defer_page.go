package frontiercheckpoint

import (
	"context"
	"time"

	bolt "go.etcd.io/bbolt"
)

func (checkpoint *FrontierCheckpoint) DeferPage(
	ctx context.Context,
	provenance []byte,
	pageURL string,
	retryAttempt uint8,
	retryAfter time.Time,
) error {
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		return err
	}
	if pageURL == "" || retryAttempt == 0 || retryAfter.IsZero() {
		return ErrInvalidPage
	}
	return checkpoint.writeTransaction(ctx, func(transaction *bolt.Tx) error {
		record, err := requiredRunRecord(transaction, provenance)
		if err != nil {
			return err
		}
		if record.Completed {
			return ErrRunCompleted
		}
		buckets, err := loadCheckpointBuckets(transaction)
		if err != nil {
			return err
		}
		row, found, err := findOutstandingPage(buckets, prefix, pageURL)
		if err != nil {
			return err
		}
		if !found || retryAttempt != row.page.RetryAttempt+1 {
			return ErrInvalidPage
		}
		row.page.RetryAttempt = retryAttempt
		row.page.RetryAfter = retryAfter.UTC()
		encoded, err := encodeRow("page", row.page)
		if err != nil {
			return err
		}
		return putRow(buckets.pages, row.key, encoded, "deferred page")
	})
}
