package frontiercheckpoint

import (
	"fmt"

	bolt "go.etcd.io/bbolt"
)

type pendingPageBudgetTrim struct {
	provenance []byte
	prefix     []byte
	keep       uint64
	removed    uint64
	complete   bool
}

func (trim *pendingPageBudgetTrim) apply(transaction *bolt.Tx) error {
	record, err := requiredRunRecord(transaction, trim.provenance)
	if err != nil {
		return err
	}
	if err := validatePendingPageBudget(record); err != nil {
		return err
	}
	if record.Pending <= trim.keep {
		trim.complete = true

		return nil
	}
	buckets, err := loadCheckpointBuckets(transaction)
	if err != nil {
		return err
	}
	wanted := min(record.Pending-trim.keep, uint64(pendingPageBudgetBatchSize))
	pageURLs, err := newestPendingPageURLs(buckets.pages, trim.prefix, wanted)
	if err != nil {
		return err
	}
	if err := removePendingPageBudgetRows(
		buckets,
		trim.prefix,
		pageURLs,
		&trim.removed,
	); err != nil {
		return err
	}
	if trim.removed != wanted || trim.removed > record.Pending {
		return fmt.Errorf("%w: pending page budget removal is incomplete", ErrCorruptCheckpoint)
	}
	record.Pending -= trim.removed
	record.BudgetDiscardedPages += trim.removed
	markCompletion(&record, buckets.pages, trim.prefix)
	trim.complete = record.Pending <= trim.keep

	return writeRunRecord(transaction, trim.provenance, record)
}

func validatePendingPageBudget(record runRecord) error {
	if record.Pages < record.Pending ||
		record.BudgetDiscardedPages > record.Pages-record.Pending {
		return fmt.Errorf("%w: invalid page budget accounting", ErrCorruptCheckpoint)
	}

	return nil
}

func removePendingPageBudgetRows(
	buckets checkpointBuckets,
	prefix []byte,
	pageURLs []string,
	removed *uint64,
) error {
	for _, pageURL := range pageURLs {
		pageRemoved, err := removeOutstandingPage(buckets, prefix, pageURL, "")
		if err != nil {
			return err
		}
		if pageRemoved {
			(*removed)++
		}
	}

	return nil
}
