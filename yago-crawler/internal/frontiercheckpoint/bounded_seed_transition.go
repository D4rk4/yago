package frontiercheckpoint

import (
	"context"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func (checkpoint *FrontierCheckpoint) FinishSeedingBatch(
	ctx context.Context,
	provenance []byte,
	tally yagocrawlcontract.CrawlRunTally,
) (bool, error) {
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		return false, err
	}
	cleanup, err := checkpoint.prepareSeedingFinish(ctx, provenance, prefix, tally)
	if err != nil || !cleanup {
		return err == nil, err
	}

	return checkpoint.deleteConsumedSeedManifestChunk(ctx, provenance, prefix)
}

func (checkpoint *FrontierCheckpoint) CancelSeedManifestBatch(
	ctx context.Context,
	provenance []byte,
) (bool, error) {
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		return false, err
	}
	done := false
	err = checkpoint.boundedWriteTransaction(ctx, func(transaction *bolt.Tx) error {
		done = false
		record, err := requiredRunRecord(transaction, provenance)
		if err != nil {
			return err
		}
		if !record.Cancelled {
			return fmt.Errorf("%w: cancellation marker is missing", ErrCorruptCheckpoint)
		}
		if !record.Seeding && !record.SeedManifest && !record.SeedManifestDeleting {
			done = true

			return nil
		}
		manifest, err := schemaBucket(transaction, seedManifestBucket)
		if err != nil {
			return err
		}
		deleted, err := deletePrefixedRows(
			manifest,
			prefix,
			seedManifestRowsPerTransaction,
		)
		if err != nil || deleted == seedManifestRowsPerTransaction {
			return err
		}
		pages, err := schemaBucket(transaction, pagesBucket)
		if err != nil {
			return err
		}
		record.Seeding = false
		record.SeedManifest = false
		record.SeedLength = 0
		record.SeedCursor = 0
		record.SeedManifestPublishing = false
		record.SeedManifestIdentity = nil
		record.SeedManifestDeleting = false
		record.SeedManifestConsumed = true
		markCompletion(&record, pages, prefix)
		done = true

		return writeRunRecord(transaction, provenance, record)
	})

	return done, err
}
