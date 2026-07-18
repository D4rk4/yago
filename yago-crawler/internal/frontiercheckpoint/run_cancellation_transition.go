package frontiercheckpoint

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	bolt "go.etcd.io/bbolt"
)

const cancellationPagesPerTransaction = 256

func (checkpoint *FrontierCheckpoint) CancelQueuedPages(
	ctx context.Context,
	provenance []byte,
	pageURLs []string,
) error {
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		return err
	}
	for _, pageURL := range pageURLs {
		if strings.TrimSpace(pageURL) == "" {
			return ErrInvalidPage
		}
	}
	if len(pageURLs) == 0 {
		return checkpoint.cancelQueuedPageChunk(ctx, provenance, prefix, nil)
	}
	for start := 0; start < len(pageURLs); start += cancellationPagesPerTransaction {
		end := min(start+cancellationPagesPerTransaction, len(pageURLs))
		if err := checkpoint.cancelQueuedPageChunk(
			ctx,
			provenance,
			prefix,
			pageURLs[start:end],
		); err != nil {
			return err
		}
	}

	return nil
}

func (checkpoint *FrontierCheckpoint) cancelQueuedPageChunk(
	ctx context.Context,
	provenance []byte,
	prefix []byte,
	pageURLs []string,
) error {
	return checkpoint.boundedWriteTransaction(ctx, func(transaction *bolt.Tx) error {
		record, err := requiredRunRecord(transaction, provenance)
		if err != nil {
			return err
		}
		if !record.Cancelled {
			return fmt.Errorf("%w: cancellation marker is missing", ErrCorruptCheckpoint)
		}
		buckets, err := loadCheckpointBuckets(transaction)
		if err != nil {
			return err
		}
		removed := uint64(0)
		for _, pageURL := range pageURLs {
			pageRemoved, err := removeOutstandingPage(buckets, prefix, pageURL, "")
			if err != nil {
				return err
			}
			if pageRemoved {
				removed++
			}
		}
		if removed > record.Pending {
			return fmt.Errorf("%w: cancelled pages exceed pending total", ErrCorruptCheckpoint)
		}
		record.Pending -= removed
		markCompletion(&record, buckets.pages, prefix)

		return writeRunRecord(transaction, provenance, record)
	})
}

func (checkpoint *FrontierCheckpoint) resumeCancelledRuns(ctx context.Context) error {
	provenances, err := checkpoint.cancelledRunProvenances(ctx)
	if err != nil {
		return err
	}
	for _, provenance := range provenances {
		prefix, err := provenancePrefix(provenance)
		if err != nil {
			return fmt.Errorf("%w: invalid cancelled provenance", ErrCorruptCheckpoint)
		}
		for {
			done, err := checkpoint.resumeCancelledRunChunk(ctx, provenance, prefix)
			if err != nil || done {
				if err != nil {
					return err
				}
				break
			}
		}
	}

	return nil
}

func (checkpoint *FrontierCheckpoint) cancelledRunProvenances(
	ctx context.Context,
) ([][]byte, error) {
	provenances := make([][]byte, 0)
	err := checkpoint.readTransaction(ctx, func(transaction *bolt.Tx) error {
		runs, err := schemaBucket(transaction, runsBucket)
		if err != nil {
			return err
		}

		return runs.ForEach(func(provenance, encoded []byte) error {
			var record runRecord
			if err := decodeRow("run", encoded, &record); err != nil {
				return err
			}
			if len(record.OrderIdentity) == 0 {
				return fmt.Errorf("%w: empty order identity", ErrCorruptCheckpoint)
			}
			if record.Cancelled && !record.Completed && !record.Deleting {
				provenances = append(provenances, bytes.Clone(provenance))
			}

			return nil
		})
	})

	return provenances, err
}

func (checkpoint *FrontierCheckpoint) resumeCancelledRunChunk(
	ctx context.Context,
	provenance []byte,
	prefix []byte,
) (bool, error) {
	done := false
	err := checkpoint.boundedWriteTransaction(ctx, func(transaction *bolt.Tx) error {
		var transitionErr error
		done, transitionErr = resumeCancelledRunTransition(transaction, provenance, prefix)

		return transitionErr
	})

	return done, err
}

func resumeCancelledRunTransition(
	transaction *bolt.Tx,
	provenance []byte,
	prefix []byte,
) (bool, error) {
	record, found, err := readRunRecord(transaction, provenance)
	if err != nil {
		return false, err
	}
	if !found || record.Completed || record.Deleting {
		return true, nil
	}
	if !record.Cancelled {
		return false, fmt.Errorf("%w: cancellation marker is missing", ErrCorruptCheckpoint)
	}
	buckets, err := loadCheckpointBuckets(transaction)
	if err != nil {
		return false, err
	}
	more, err := cancelRunRows(transaction, buckets, prefix, &record)
	if err != nil {
		return false, err
	}
	if more {
		return false, writeRunRecord(transaction, provenance, record)
	}
	if record.Pending != 0 {
		return false, fmt.Errorf("%w: cancelled run retains pending pages", ErrCorruptCheckpoint)
	}
	clearCancelledSeedManifest(&record)
	markCompletion(&record, buckets.pages, prefix)

	return true, writeRunRecord(transaction, provenance, record)
}

func cancelRunRows(
	transaction *bolt.Tx,
	buckets checkpointBuckets,
	prefix []byte,
	record *runRecord,
) (bool, error) {
	pageURLs, err := prefixedPageURLs(
		buckets.pages,
		prefix,
		cancellationPagesPerTransaction,
	)
	if err != nil {
		return false, err
	}
	removed, err := removeHostPages(buckets, prefix, "", pageURLs)
	if err != nil {
		return false, err
	}
	if removed > record.Pending {
		return false, fmt.Errorf("%w: cancelled pages exceed pending total", ErrCorruptCheckpoint)
	}
	record.Pending -= removed
	remaining := cancellationPagesPerTransaction - len(pageURLs)
	manifestDeleted, err := cancelManifestRows(transaction, prefix, remaining)
	if err != nil {
		return false, err
	}

	return len(pageURLs) == cancellationPagesPerTransaction || manifestDeleted == remaining, nil
}

func cancelManifestRows(
	transaction *bolt.Tx,
	prefix []byte,
	limit int,
) (int, error) {
	if limit == 0 {
		return 0, nil
	}
	manifest, err := schemaBucket(transaction, seedManifestBucket)
	if err != nil {
		return 0, err
	}

	return deletePrefixedRows(manifest, prefix, limit)
}

func clearCancelledSeedManifest(record *runRecord) {
	record.Seeding = false
	record.SeedManifest = false
	record.SeedLength = 0
	record.SeedCursor = 0
	record.SeedManifestPublishing = false
	record.SeedManifestIdentity = nil
	record.SeedManifestDeleting = false
	record.SeedManifestConsumed = true
}

func prefixedPageURLs(bucket *bolt.Bucket, prefix []byte, limit int) ([]string, error) {
	pageURLs := make([]string, 0, limit)
	cursor := bucket.Cursor()
	for key, encoded := cursor.Seek(prefix); key != nil && bytes.HasPrefix(key, prefix) && len(pageURLs) < limit; key, encoded = cursor.Next() {
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
	}

	return pageURLs, nil
}
