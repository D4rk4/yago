package frontiercheckpoint

import (
	"context"
	"fmt"
	"strings"

	bolt "go.etcd.io/bbolt"
)

func (checkpoint *FrontierCheckpoint) RecordHostState(
	ctx context.Context,
	provenance []byte,
	host string,
	progress HostProgress,
	droppedURLs []string,
) error {
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		return err
	}
	if strings.TrimSpace(host) == "" {
		return ErrInvalidPage
	}
	if err := validateHostPaceProgress(progress); err != nil {
		return err
	}
	for _, pageURL := range droppedURLs {
		if strings.TrimSpace(pageURL) == "" {
			return ErrInvalidPage
		}
	}
	if len(droppedURLs) > 0 && !progress.Retired {
		return ErrInvalidHostState
	}
	current := false
	err = checkpoint.writeTransaction(ctx, func(transaction *bolt.Tx) error {
		current = false
		record, err := requiredRunRecord(transaction, provenance)
		if err != nil {
			return err
		}
		buckets, err := loadCheckpointBuckets(transaction)
		if err != nil {
			return err
		}
		if err := validateHostPagesInTransaction(
			buckets,
			prefix,
			host,
			droppedURLs,
			record.Pending,
		); err != nil {
			return err
		}
		current, err = applyHostProgress(
			transaction,
			buckets,
			prefix,
			host,
			progress,
		)
		if err != nil {
			return err
		}
		markCompletion(&record, buckets.pages, prefix)
		return writeRunRecord(transaction, provenance, record)
	})
	if err != nil || !current {
		return err
	}

	return checkpoint.removeHostPagesInChunks(
		ctx,
		hostPageRemoval{
			provenance: provenance,
			prefix:     prefix,
			host:       host,
			generation: progress.Generation,
			pageURLs:   droppedURLs,
		},
	)
}

func setHostState(
	bucket *bolt.Bucket,
	prefix []byte,
	host string,
	progress HostProgress,
) (bool, error) {
	record, err := readHostRecord(bucket, prefix, host)
	if err != nil {
		return false, err
	}
	if progress.Generation < record.Generation {
		return false, nil
	}
	if progress.Generation == record.Generation && progress.Generation != 0 {
		if progress.Failures != record.Failures || progress.Retired != record.Retired {
			return false, fmt.Errorf(
				"%w: conflicting host outcome generation",
				ErrCorruptCheckpoint,
			)
		}
		return true, nil
	}
	if progress.Retired && (!record.Retired || progress.Generation != record.Generation) {
		record.RetirementCursor = 0
		record.RetirementScanned = false
	}
	if !progress.Retired {
		record.RetirementCursor = 0
		record.RetirementScanned = false
	}
	record.Failures = progress.Failures
	record.Retired = progress.Retired
	record.Generation = progress.Generation
	return true, writeHostRecord(bucket, prefix, host, record)
}

func removeHostPages(
	buckets checkpointBuckets,
	prefix []byte,
	host string,
	pageURLs []string,
) (uint64, error) {
	var removed uint64
	for _, pageURL := range pageURLs {
		pageRemoved, err := removeOutstandingPage(buckets, prefix, pageURL, host)
		if err != nil {
			return 0, err
		}
		if pageRemoved {
			removed++
		}
	}
	return removed, nil
}
