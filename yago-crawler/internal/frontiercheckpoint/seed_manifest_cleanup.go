package frontiercheckpoint

import (
	"bytes"
	"context"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func (checkpoint *FrontierCheckpoint) prepareSeedingFinish(
	ctx context.Context,
	provenance []byte,
	prefix []byte,
	tally yagocrawlcontract.CrawlRunTally,
) (bool, error) {
	cleanup := false
	err := checkpoint.writeTransaction(ctx, func(transaction *bolt.Tx) error {
		cleanup = false
		record, err := requiredRunRecord(transaction, provenance)
		if err != nil {
			return err
		}
		if record.SeedManifestPublishing {
			return ErrSeedManifestMissing
		}
		if record.SeedManifestDeleting {
			cleanup = true

			return nil
		}
		if record.SeedManifest {
			if record.SeedCursor != record.SeedLength {
				return ErrInvalidSeedBatch
			}
			record.SeedManifest = false
			record.SeedManifestDeleting = true
			cleanup = true

			return writeRunRecord(transaction, provenance, record)
		}
		pages, err := schemaBucket(transaction, pagesBucket)
		if err != nil {
			return err
		}
		if !record.SeedManifestConsumed {
			record.Tally, err = addRunTally(record.Tally, tally)
			if err != nil {
				return err
			}
		}
		record.Seeding = false
		markCompletion(&record, pages, prefix)

		return writeRunRecord(transaction, provenance, record)
	})

	return cleanup, err
}

func (checkpoint *FrontierCheckpoint) deleteConsumedSeedManifest(
	ctx context.Context,
	provenance []byte,
	prefix []byte,
) error {
	for {
		done, err := checkpoint.deleteConsumedSeedManifestChunk(ctx, provenance, prefix)
		if err != nil || done {
			return err
		}
	}
}

func (checkpoint *FrontierCheckpoint) deleteConsumedSeedManifestChunk(
	ctx context.Context,
	provenance []byte,
	prefix []byte,
) (bool, error) {
	done := false
	err := checkpoint.boundedWriteTransaction(ctx, func(transaction *bolt.Tx) error {
		done = false
		record, found, err := readRunRecord(transaction, provenance)
		if err != nil {
			return err
		}
		if !found {
			done = true

			return nil
		}
		if !record.SeedManifestDeleting {
			if record.SeedManifestConsumed {
				done = true

				return nil
			}

			return fmt.Errorf("%w: seed manifest deletion marker is missing", ErrCorruptCheckpoint)
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
		record.SeedManifestDeleting = false
		record.SeedManifestConsumed = true
		record.SeedLength = 0
		record.SeedCursor = 0
		record.Seeding = false
		markCompletion(&record, pages, prefix)
		done = true

		return writeRunRecord(transaction, provenance, record)
	})

	return done, err
}

func (checkpoint *FrontierCheckpoint) resumeSeedManifestTransitions(ctx context.Context) error {
	publications, deletions, err := checkpoint.seedManifestTransitionProvenances(ctx)
	if err != nil {
		return err
	}
	for _, provenance := range publications {
		prefix, err := provenancePrefix(provenance)
		if err != nil {
			return fmt.Errorf(
				"%w: invalid seed manifest publication provenance",
				ErrCorruptCheckpoint,
			)
		}
		if err := checkpoint.discardSeedManifestPublication(ctx, provenance, prefix); err != nil {
			return err
		}
	}
	for _, provenance := range deletions {
		prefix, err := provenancePrefix(provenance)
		if err != nil {
			return fmt.Errorf("%w: invalid seed manifest deletion provenance", ErrCorruptCheckpoint)
		}
		if err := checkpoint.deleteConsumedSeedManifest(ctx, provenance, prefix); err != nil {
			return err
		}
	}

	return nil
}

func (checkpoint *FrontierCheckpoint) seedManifestTransitionProvenances(
	ctx context.Context,
) ([][]byte, [][]byte, error) {
	var publications [][]byte
	var deletions [][]byte
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
			if record.SeedManifestPublishing && record.SeedManifestDeleting {
				return fmt.Errorf(
					"%w: seed manifest has conflicting transitions",
					ErrCorruptCheckpoint,
				)
			}
			if record.SeedManifestPublishing {
				publications = append(publications, bytes.Clone(provenance))
			}
			if record.SeedManifestDeleting {
				deletions = append(deletions, bytes.Clone(provenance))
			}

			return nil
		})
	})

	return publications, deletions, err
}

func (checkpoint *FrontierCheckpoint) discardSeedManifestPublication(
	ctx context.Context,
	provenance []byte,
	prefix []byte,
) error {
	for {
		done, err := checkpoint.discardSeedManifestPublicationChunk(ctx, provenance, prefix)
		if err != nil || done {
			return err
		}
	}
}

func (checkpoint *FrontierCheckpoint) discardSeedManifestPublicationChunk(
	ctx context.Context,
	provenance []byte,
	prefix []byte,
) (bool, error) {
	done := false
	err := checkpoint.boundedWriteTransaction(ctx, func(transaction *bolt.Tx) error {
		done = false
		record, found, err := readRunRecord(transaction, provenance)
		if err != nil {
			return err
		}
		if !found {
			done = true

			return nil
		}
		if !record.SeedManifestPublishing {
			return fmt.Errorf(
				"%w: seed manifest publication marker is missing",
				ErrCorruptCheckpoint,
			)
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
		done = true

		return deleteRow(transaction.Bucket(runsBucket), provenance, "seed manifest publication")
	})

	return done, err
}
