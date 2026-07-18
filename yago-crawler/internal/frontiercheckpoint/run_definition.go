package frontiercheckpoint

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func (checkpoint *FrontierCheckpoint) Status(
	ctx context.Context,
	provenance []byte,
	orderIdentity []byte,
) (RunStatus, error) {
	if _, err := provenancePrefix(provenance); err != nil {
		return RunMissing, err
	}
	if len(orderIdentity) == 0 {
		return RunMissing, ErrInvalidIdentity
	}
	status := RunMissing
	err := checkpoint.readTransaction(ctx, func(transaction *bolt.Tx) error {
		record, found, err := readRunRecord(transaction, provenance)
		if err != nil || !found {
			return err
		}
		if !bytes.Equal(record.OrderIdentity, orderIdentity) {
			return ErrProvenanceCollision
		}
		if record.Deleting {
			return ErrRunDeleting
		}
		status = RunActive
		if record.Completed {
			status = RunCompleted
		}
		return nil
	})
	return status, err
}

func (checkpoint *FrontierCheckpoint) Begin(
	ctx context.Context,
	provenance []byte,
	orderIdentity []byte,
	priority yagocrawlcontract.CrawlOrderPriority,
) error {
	if _, err := provenancePrefix(provenance); err != nil {
		return err
	}
	if len(orderIdentity) == 0 {
		return ErrInvalidIdentity
	}
	return checkpoint.writeTransaction(ctx, func(transaction *bolt.Tx) error {
		record, found, err := readRunRecord(transaction, provenance)
		if err != nil {
			return err
		}
		if found {
			return validateRunIdentity(record, orderIdentity, priority)
		}
		return writeRunRecord(transaction, provenance, runRecord{
			OrderIdentity: append([]byte(nil), orderIdentity...),
			Priority:      priority,
			Seeding:       true,
		})
	})
}

func validateRunIdentity(
	record runRecord,
	orderIdentity []byte,
	priority yagocrawlcontract.CrawlOrderPriority,
) error {
	if !bytes.Equal(record.OrderIdentity, orderIdentity) || record.Priority != priority {
		return ErrProvenanceCollision
	}
	if record.Deleting {
		return ErrRunDeleting
	}
	return nil
}

func readRunRecord(
	transaction *bolt.Tx,
	provenance []byte,
) (runRecord, bool, error) {
	bucket, err := schemaBucket(transaction, runsBucket)
	if err != nil {
		return runRecord{}, false, err
	}
	encoded := bucket.Get(provenance)
	if encoded == nil {
		return runRecord{}, false, nil
	}
	var record runRecord
	if err := decodeRow("run", encoded, &record); err != nil {
		return runRecord{}, false, err
	}
	if len(record.OrderIdentity) == 0 {
		return runRecord{}, false, fmt.Errorf("%w: empty order identity", ErrCorruptCheckpoint)
	}
	return record, true, nil
}

func requiredRunRecord(transaction *bolt.Tx, provenance []byte) (runRecord, error) {
	record, found, err := readRunRecord(transaction, provenance)
	if err != nil {
		return runRecord{}, err
	}
	if !found {
		return runRecord{}, ErrRunNotFound
	}
	if record.Deleting {
		return runRecord{}, ErrRunDeleting
	}
	return record, nil
}

func writeRunRecord(transaction *bolt.Tx, provenance []byte, record runRecord) error {
	bucket, err := schemaBucket(transaction, runsBucket)
	if err != nil {
		return err
	}
	encoded, encodingError := encodeRow("run", record)
	return errors.Join(
		encodingError,
		putRow(bucket, provenance, encoded, "run"),
	)
}
