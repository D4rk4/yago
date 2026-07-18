package frontiercheckpoint

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const seedManifestRowsPerTransaction = SeedAdmissionBatchSize

type seedManifestPublication struct {
	provenance       []byte
	prefix           []byte
	orderIdentity    []byte
	priority         yagocrawlcontract.CrawlOrderPriority
	encodedPages     [][]byte
	manifestIdentity []byte
	manifestLength   uint64
}

func (checkpoint *FrontierCheckpoint) publishSeedManifest(
	ctx context.Context,
	publication seedManifestPublication,
) error {
	publishing, err := checkpoint.prepareSeedManifestPublication(
		ctx,
		publication,
	)
	if err != nil || !publishing {
		return err
	}
	for {
		done, err := checkpoint.stageSeedManifestChunk(
			ctx,
			publication,
		)
		if err != nil {
			return err
		}
		if done {
			break
		}
	}

	return checkpoint.completeSeedManifestPublication(
		ctx,
		publication.provenance,
		publication.manifestIdentity,
		publication.manifestLength,
	)
}

func identifySeedManifest(encodedPages [][]byte) []byte {
	identity := sha256.New()
	length := make([]byte, 8)
	binary.BigEndian.PutUint64(length, uint64(len(encodedPages)))
	_, _ = identity.Write(length)
	for _, encoded := range encodedPages {
		binary.BigEndian.PutUint64(length, uint64(len(encoded)))
		_, _ = identity.Write(length)
		_, _ = identity.Write(encoded)
	}

	return identity.Sum(nil)
}

func (checkpoint *FrontierCheckpoint) prepareSeedManifestPublication(
	ctx context.Context,
	publication seedManifestPublication,
) (bool, error) {
	publishing := false
	err := checkpoint.writeTransaction(ctx, func(transaction *bolt.Tx) error {
		publishing = false
		record, found, err := readRunRecord(transaction, publication.provenance)
		if err != nil {
			return err
		}
		if found {
			if err := validateRunIdentity(
				record,
				publication.orderIdentity,
				publication.priority,
			); err != nil {
				return err
			}
			if record.SeedManifestPublishing {
				if record.SeedLength != publication.manifestLength ||
					!bytes.Equal(record.SeedManifestIdentity, publication.manifestIdentity) {
					return ErrProvenanceCollision
				}
				publishing = true

				return nil
			}
			if record.Seeding && !record.SeedManifest {
				return ErrSeedManifestMissing
			}

			return nil
		}
		publishing = true

		return writeRunRecord(transaction, publication.provenance, runRecord{
			OrderIdentity:          append([]byte(nil), publication.orderIdentity...),
			Priority:               publication.priority,
			Seeding:                true,
			SeedLength:             publication.manifestLength,
			SeedManifestPublishing: true,
			SeedManifestIdentity:   append([]byte(nil), publication.manifestIdentity...),
		})
	})

	return publishing, err
}

func (checkpoint *FrontierCheckpoint) stageSeedManifestChunk(
	ctx context.Context,
	publication seedManifestPublication,
) (bool, error) {
	done := false
	err := checkpoint.boundedWriteTransaction(ctx, func(transaction *bolt.Tx) error {
		var stageErr error
		done, stageErr = stageSeedManifestRows(transaction, publication)

		return stageErr
	})

	return done, err
}

func stageSeedManifestRows(
	transaction *bolt.Tx,
	publication seedManifestPublication,
) (bool, error) {
	record, err := requiredRunRecord(transaction, publication.provenance)
	if err != nil {
		return false, err
	}
	if record.SeedManifest && !record.SeedManifestPublishing {
		return true, nil
	}
	if !record.SeedManifestPublishing ||
		record.SeedLength != publication.manifestLength ||
		!bytes.Equal(record.SeedManifestIdentity, publication.manifestIdentity) {
		return false, fmt.Errorf("%w: seed manifest publication changed", ErrCorruptCheckpoint)
	}
	if record.SeedCursor > record.SeedLength {
		return false, fmt.Errorf(
			"%w: seed manifest publication cursor exceeds its length",
			ErrCorruptCheckpoint,
		)
	}
	if record.SeedCursor == record.SeedLength {
		return true, nil
	}
	manifest, err := schemaBucket(transaction, seedManifestBucket)
	if err != nil {
		return false, err
	}
	end := min(
		record.SeedCursor+seedManifestRowsPerTransaction,
		record.SeedLength,
	)
	if err := writeSeedManifestRows(manifest, publication, record.SeedCursor, end); err != nil {
		return false, err
	}
	record.SeedCursor = end

	return end == record.SeedLength,
		writeRunRecord(transaction, publication.provenance, record)
}

func writeSeedManifestRows(
	manifest *bolt.Bucket,
	publication seedManifestPublication,
	start uint64,
	end uint64,
) error {
	for index := start; index < end; index++ {
		key := sequenceRowKey(publication.prefix, index+1)
		if manifest.Get(key) != nil {
			return fmt.Errorf(
				"%w: seed manifest publication overlaps persisted rows",
				ErrCorruptCheckpoint,
			)
		}
		if err := putRow(
			manifest,
			key,
			publication.encodedPages[index],
			"seed manifest page",
		); err != nil {
			return err
		}
	}

	return nil
}

func (checkpoint *FrontierCheckpoint) completeSeedManifestPublication(
	ctx context.Context,
	provenance []byte,
	manifestIdentity []byte,
	manifestLength uint64,
) error {
	return checkpoint.writeTransaction(ctx, func(transaction *bolt.Tx) error {
		record, err := requiredRunRecord(transaction, provenance)
		if err != nil {
			return err
		}
		if record.SeedManifest && !record.SeedManifestPublishing {
			return nil
		}
		if !record.SeedManifestPublishing || record.SeedCursor != manifestLength ||
			record.SeedLength != manifestLength ||
			!bytes.Equal(record.SeedManifestIdentity, manifestIdentity) {
			return fmt.Errorf("%w: seed manifest publication is incomplete", ErrCorruptCheckpoint)
		}
		record.SeedManifestPublishing = false
		record.SeedManifestIdentity = nil
		record.SeedManifest = true
		record.SeedCursor = 0

		return writeRunRecord(transaction, provenance, record)
	})
}
