package frontiercheckpoint

import (
	"bytes"
	"context"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

const deletionRowsPerTransaction = 256

func (checkpoint *FrontierCheckpoint) Delete(ctx context.Context, provenance []byte) error {
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		return err
	}
	found, err := checkpoint.markRunDeleting(ctx, provenance)
	if err != nil || !found {
		return err
	}
	return checkpoint.deleteMarkedRun(ctx, provenance, prefix)
}

func (checkpoint *FrontierCheckpoint) markRunDeleting(
	ctx context.Context,
	provenance []byte,
) (bool, error) {
	found := false
	err := checkpoint.writeTransaction(ctx, func(transaction *bolt.Tx) error {
		found = false
		record, exists, err := readRunRecord(transaction, provenance)
		if err != nil || !exists {
			return err
		}
		found = true
		if record.Deleting {
			return nil
		}
		record.Deleting = true
		return writeRunRecord(transaction, provenance, record)
	})
	return found, err
}

func (checkpoint *FrontierCheckpoint) deleteMarkedRun(
	ctx context.Context,
	provenance []byte,
	prefix []byte,
) error {
	for {
		done, err := checkpoint.deleteMarkedRunChunk(ctx, provenance, prefix)
		if err != nil || done {
			return err
		}
	}
}

func (checkpoint *FrontierCheckpoint) deleteMarkedRunChunk(
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
		if !record.Deleting {
			return fmt.Errorf("%w: deletion marker is missing", ErrCorruptCheckpoint)
		}
		buckets, err := runLeafBuckets(transaction)
		if err != nil {
			return err
		}
		remaining := deletionRowsPerTransaction
		for _, bucket := range buckets {
			deleted, err := deletePrefixedRows(bucket, prefix, remaining)
			remaining -= deleted
			if err != nil || remaining == 0 {
				return err
			}
		}
		done = true
		return deleteRow(transaction.Bucket(runsBucket), provenance, "run")
	})
	return done, err
}

func deletePrefixedRows(bucket *bolt.Bucket, prefix []byte, limit int) (int, error) {
	keys := make([][]byte, 0, limit)
	cursor := bucket.Cursor()
	for key, _ := cursor.Seek(prefix); key != nil && bytes.HasPrefix(key, prefix) && len(keys) < limit; key, _ = cursor.Next() {
		keys = append(keys, bytes.Clone(key))
	}
	for _, key := range keys {
		if err := deleteRow(bucket, key, "rows"); err != nil {
			return 0, err
		}
	}
	return len(keys), nil
}

func (checkpoint *FrontierCheckpoint) resumeDeletions(ctx context.Context) error {
	provenances, err := checkpoint.deletingProvenances(ctx)
	if err != nil {
		return err
	}
	for _, provenance := range provenances {
		prefix, err := provenancePrefix(provenance)
		if err != nil {
			return fmt.Errorf("%w: invalid deleting provenance", ErrCorruptCheckpoint)
		}
		if err := checkpoint.deleteMarkedRun(ctx, provenance, prefix); err != nil {
			return err
		}
	}
	return nil
}

func (checkpoint *FrontierCheckpoint) deletingProvenances(ctx context.Context) ([][]byte, error) {
	var provenances [][]byte
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
			if !record.Deleting {
				return nil
			}
			if len(record.OrderIdentity) == 0 {
				return fmt.Errorf("%w: empty order identity", ErrCorruptCheckpoint)
			}
			provenances = append(provenances, bytes.Clone(provenance))
			return nil
		})
	})
	return provenances, err
}
