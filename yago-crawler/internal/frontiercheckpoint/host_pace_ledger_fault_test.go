package frontiercheckpoint

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlpace"
)

func validPaceState(generation uint64) crawlpace.HostState {
	return crawlpace.HostState{
		NextDueAt:  time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC),
		Generation: generation,
	}
}

func TestHostPaceLoadRejectsSchemaCorruption(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		bucket []byte
	}{
		{name: "metadata", bucket: metadataBucket},
		{name: "paces", bucket: hostPacesBucket},
		{name: "order", bucket: hostPaceOrderBucket},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
			deleteSchemaBucket(t, checkpoint, testCase.bucket)
			if _, err := checkpoint.HostPaces(
				testContext,
				1,
			); !errors.Is(
				err,
				ErrCorruptCheckpoint,
			) {
				t.Fatalf("missing host pace bucket error = %v", err)
			}
		})
	}
}

type hostPaceLedgerFault struct {
	name   string
	mutate func(*bolt.Tx) error
}

var hostPaceLedgerFaults = []hostPaceLedgerFault{
	{name: "total width", mutate: func(transaction *bolt.Tx) error {
		return transaction.Bucket(metadataBucket).Put(hostPaceTotalKey, []byte{1})
	}},
	{name: "pace encoding", mutate: func(transaction *bolt.Tx) error {
		if err := transaction.Bucket(hostPacesBucket).
			Put([]byte("bad.example"), []byte("{")); err != nil {
			return wrapDatabaseError("write corrupt pace fixture", err)
		}
		return transaction.Bucket(metadataBucket).Put(hostPaceTotalKey, sequenceValue(1))
	}},
	{name: "pace value", mutate: func(transaction *bolt.Tx) error {
		encoded, err := encodeRow("pace", hostPaceRecord{})
		if err != nil {
			return err
		}
		if err := transaction.Bucket(hostPacesBucket).
			Put([]byte("bad.example"), encoded); err != nil {
			return wrapDatabaseError("write invalid pace fixture", err)
		}
		return transaction.Bucket(metadataBucket).Put(hostPaceTotalKey, sequenceValue(1))
	}},
	{name: "order size", mutate: func(transaction *bolt.Tx) error {
		encoded, err := encodeRow("pace", hostPaceRecord{State: validPaceState(1), Sequence: 1})
		if err != nil {
			return err
		}
		if err := transaction.Bucket(hostPacesBucket).
			Put([]byte("bad.example"), encoded); err != nil {
			return wrapDatabaseError("write pace fixture", err)
		}
		return transaction.Bucket(metadataBucket).Put(hostPaceTotalKey, sequenceValue(1))
	}},
	{name: "order key", mutate: func(transaction *bolt.Tx) error {
		encoded, err := encodeRow("pace", hostPaceRecord{State: validPaceState(1), Sequence: 1})
		if err != nil {
			return err
		}
		if err := transaction.Bucket(hostPacesBucket).
			Put([]byte("bad.example"), encoded); err != nil {
			return wrapDatabaseError("write pace fixture", err)
		}
		if err := transaction.Bucket(hostPaceOrderBucket).
			Put([]byte{1}, []byte("bad.example")); err != nil {
			return wrapDatabaseError("write pace order fixture", err)
		}
		return transaction.Bucket(metadataBucket).Put(hostPaceTotalKey, sequenceValue(1))
	}},
	{name: "order host", mutate: func(transaction *bolt.Tx) error {
		if err := transaction.Bucket(hostPaceOrderBucket).
			Put(sequenceValue(1), []byte{}); err != nil {
			return wrapDatabaseError("write empty pace order fixture", err)
		}
		return transaction.Bucket(metadataBucket).Put(hostPaceTotalKey, sequenceValue(1))
	}},
	{name: "order missing pace", mutate: func(transaction *bolt.Tx) error {
		if err := transaction.Bucket(hostPaceOrderBucket).
			Put(sequenceValue(1), []byte("missing.example")); err != nil {
			return wrapDatabaseError("write missing pace order fixture", err)
		}
		return transaction.Bucket(metadataBucket).Put(hostPaceTotalKey, sequenceValue(1))
	}},
	{name: "order sequence mismatch", mutate: func(transaction *bolt.Tx) error {
		encoded, err := encodeRow("pace", hostPaceRecord{State: validPaceState(1), Sequence: 2})
		if err != nil {
			return err
		}
		if err := transaction.Bucket(hostPacesBucket).
			Put([]byte("bad.example"), encoded); err != nil {
			return wrapDatabaseError("write mismatched pace fixture", err)
		}
		if err := transaction.Bucket(hostPaceOrderBucket).
			Put(sequenceValue(1), []byte("bad.example")); err != nil {
			return wrapDatabaseError("write mismatched pace order fixture", err)
		}
		return transaction.Bucket(metadataBucket).Put(hostPaceTotalKey, sequenceValue(1))
	}},
}

func TestHostPaceLoadRejectsLedgerCorruption(t *testing.T) {
	for _, testCase := range hostPaceLedgerFaults {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
			mutateCheckpoint(t, checkpoint, testCase.mutate)
			if _, err := checkpoint.HostPaces(
				testContext,
				1,
			); !errors.Is(
				err,
				ErrCorruptCheckpoint,
			) {
				t.Fatalf("corrupt host pace load error = %v", err)
			}
		})
	}
}

func TestHostPaceRecordRejectsStorageCorruptionAndRollsBack(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*bolt.Tx, []byte) error
	}{
		{name: "total", mutate: func(transaction *bolt.Tx, _ []byte) error {
			return transaction.Bucket(metadataBucket).Put(hostPaceTotalKey, []byte{1})
		}},
		{name: "sequence", mutate: func(transaction *bolt.Tx, _ []byte) error {
			return transaction.Bucket(metadataBucket).Put(hostPaceSequenceKey, []byte{1})
		}},
		{name: "sequence overflow", mutate: func(transaction *bolt.Tx, _ []byte) error {
			return transaction.Bucket(metadataBucket).Put(hostPaceSequenceKey, sequenceValue(math.MaxUint64))
		}},
		{name: "existing encoding", mutate: func(transaction *bolt.Tx, host []byte) error {
			return transaction.Bucket(hostPacesBucket).Put(host, []byte("{"))
		}},
		{name: "existing order mismatch", mutate: func(transaction *bolt.Tx, host []byte) error {
			encoded, err := encodeRow("pace", hostPaceRecord{State: validPaceState(1), Sequence: 1})
			if err != nil {
				return err
			}
			if err := transaction.Bucket(hostPacesBucket).Put(host, encoded); err != nil {
				return wrapDatabaseError("write existing pace fixture", err)
			}
			return transaction.Bucket(metadataBucket).Put(hostPaceTotalKey, sequenceValue(1))
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
			host := []byte("pace.example")
			mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
				return testCase.mutate(transaction, host)
			})
			err := checkpoint.writeTransaction(testContext, func(transaction *bolt.Tx) error {
				return recordHostPace(transaction, string(host), validPaceState(2), 1)
			})
			if !errors.Is(err, ErrCorruptCheckpoint) {
				t.Fatalf("corrupt host pace record error = %v", err)
			}
		})
	}
}

func TestHostPaceEvictionRejectsBrokenOldestEntry(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*bolt.Tx) error
	}{
		{name: "missing order", mutate: func(transaction *bolt.Tx) error {
			return transaction.Bucket(metadataBucket).Put(hostPaceTotalKey, sequenceValue(2))
		}},
		{name: "missing pace", mutate: func(transaction *bolt.Tx) error {
			if err := transaction.Bucket(hostPaceOrderBucket).Put(sequenceValue(1), []byte("missing.example")); err != nil {
				return wrapDatabaseError("write missing pace order fixture", err)
			}
			return transaction.Bucket(metadataBucket).Put(hostPaceTotalKey, sequenceValue(2))
		}},
		{name: "sequence mismatch", mutate: func(transaction *bolt.Tx) error {
			encoded, err := encodeRow("pace", hostPaceRecord{State: validPaceState(1), Sequence: 2})
			if err != nil {
				return err
			}
			if err := transaction.Bucket(hostPacesBucket).Put([]byte("pace.example"), encoded); err != nil {
				return wrapDatabaseError("write sequence-mismatch pace fixture", err)
			}
			if err := transaction.Bucket(hostPaceOrderBucket).Put(sequenceValue(1), []byte("pace.example")); err != nil {
				return wrapDatabaseError("write sequence-mismatch order fixture", err)
			}
			return transaction.Bucket(metadataBucket).Put(hostPaceTotalKey, sequenceValue(2))
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
			mutateCheckpoint(t, checkpoint, testCase.mutate)
			if _, err := checkpoint.HostPaces(
				testContext,
				1,
			); !errors.Is(
				err,
				ErrCorruptCheckpoint,
			) {
				t.Fatalf("broken eviction error = %v", err)
			}
		})
	}
}

func TestHostPaceStorageRejectsOversizedHostAtomically(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	err := checkpoint.writeTransaction(testContext, func(transaction *bolt.Tx) error {
		return recordHostPace(
			transaction,
			strings.Repeat("h", bolt.MaxKeySize+1),
			validPaceState(1),
			1,
		)
	})
	if err == nil {
		t.Fatal("oversized host pace key succeeded")
	}
	states, err := checkpoint.HostPaces(testContext, 1)
	if err != nil || len(states) != 0 {
		t.Fatalf("host pace rollback = %+v, %v", states, err)
	}
}
