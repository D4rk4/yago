package crawlbroker

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestLeaseSettlementRecordCodecRejectsInvalidLength(t *testing.T) {
	if _, err := (leaseSettlementRecordCodec{}).Decode([]byte{1}); err == nil {
		t.Fatal("invalid settlement record decoded")
	}
}

func TestLeaseSettlementRecordCodecReadsLegacySequence(t *testing.T) {
	record, err := (leaseSettlementRecordCodec{}).Decode([]byte{
		byte(leaseSettlementRequeued), 0, 0, 0, 0, 0, 0, 0, 7,
	})
	if err != nil || record.Outcome != leaseSettlementRequeued || record.Sequence != 7 ||
		record.SettledAtUnixNano != 0 {
		t.Fatalf("legacy settlement = %+v err=%v", record, err)
	}
}

func TestLeaseSettlementIdentityCodecRejectsInvalidRecord(t *testing.T) {
	if _, err := (leaseSettlementIdentityCodec{}).Decode([]byte{0}); err == nil {
		t.Fatal("invalid settlement identity decoded")
	}
}

func TestLeaseSettlementHistoryRejectsCorruption(t *testing.T) {
	fixture := scriptedQueue(t)
	fixture.engine.buckets[leaseSettlementBucket]["corrupt"] = []byte{1}
	if err := fixture.queue.ackLease(t.Context(), "corrupt"); err == nil {
		t.Fatal("corrupt settlement history was accepted")
	}
}

func TestTerminalLeaseSettlementSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open first storage: %v", err)
	}
	first, err := newDurableOrderQueue(storage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("open first queue: %v", err)
	}
	leaseID := leaseOne(t, first, "terminal", "worker")
	if err := first.ackLease(t.Context(), leaseID); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close first storage: %v", err)
	}
	storage, err = boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open second storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	second, err := newDurableOrderQueue(storage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("open second queue: %v", err)
	}
	if err := second.ackLease(t.Context(), leaseID); err != nil {
		t.Fatalf("duplicate ack after restart: %v", err)
	}
	if err := second.deferLease(
		t.Context(),
		leaseID,
	); !errors.Is(
		err,
		errLeaseDispositionConflict,
	) {
		t.Fatalf("nak after terminal settlement = %v", err)
	}
}

func TestRecordLeaseSettlementIsIdempotentAndRejectsConflict(t *testing.T) {
	queue := memQueue(t)
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := queue.recordLeaseSettlement(
			tx,
			"lease",
			leaseSettlementAcknowledged,
		); err != nil {
			return err
		}
		if err := queue.recordLeaseSettlement(
			tx,
			"lease",
			leaseSettlementAcknowledged,
		); err != nil {
			return err
		}

		return queue.recordLeaseSettlement(tx, "lease", leaseSettlementRequeued)
	}); !errors.Is(err, errLeaseDispositionConflict) {
		t.Fatalf("conflicting history = %v", err)
	}
}

func TestRecordLeaseSettlementSurfacesStorageErrors(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*scriptedQueueFixture)
	}{
		{
			name: "read existing",
			prepare: func(fixture *scriptedQueueFixture) {
				fixture.engine.buckets[leaseSettlementBucket]["lease"] = []byte{1}
			},
		},
		{
			name: "read sequence",
			prepare: func(fixture *scriptedQueueFixture) {
				fixture.engine.buckets[seqBucket][string(leaseSettlementNextKey)] = []byte{1}
			},
		},
		{
			name: "read migration",
			prepare: func(fixture *scriptedQueueFixture) {
				fixture.engine.buckets[seqBucket][string(leaseSettlementMigrationNextKey)] = []byte{1}
			},
		},
		{
			name: "store settlement",
			prepare: func(fixture *scriptedQueueFixture) {
				fixture.engine.putErrors[leaseSettlementBucket] = errors.New("put failed")
			},
		},
		{
			name: "store index",
			prepare: func(fixture *scriptedQueueFixture) {
				fixture.engine.putErrors[leaseSettlementOrderBucket] = errors.New("put failed")
			},
		},
		{
			name: "store expiry",
			prepare: func(fixture *scriptedQueueFixture) {
				fixture.engine.putErrors[leaseSettlementExpiryBucket] = errors.New("put failed")
			},
		},
		{
			name: "advance sequence",
			prepare: func(fixture *scriptedQueueFixture) {
				fixture.engine.putKeyErrors[seqBucket] = map[string]error{
					string(leaseSettlementNextKey): errors.New("put failed"),
				}
			},
		},
		{
			name: "advance migration",
			prepare: func(fixture *scriptedQueueFixture) {
				fixture.engine.putKeyErrors[seqBucket] = map[string]error{
					string(leaseSettlementMigrationNextKey): errors.New("put failed"),
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := scriptedQueue(t)
			test.prepare(&fixture)
			err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
				return fixture.queue.recordLeaseSettlement(
					tx,
					"lease",
					leaseSettlementAcknowledged,
				)
			})
			if err == nil {
				t.Fatal("storage failure was hidden")
			}
		})
	}
}

func TestLeaseSettlementHistoryRetainsEveryRecordBeyondFormerWindow(t *testing.T) {
	queue := memQueue(t)
	if err := recordExtendedLeaseSettlementHistory(t, queue); err != nil {
		t.Fatalf("record settlement history: %v", err)
	}
	if err := assertExtendedLeaseSettlementHistory(t, queue); err != nil {
		t.Fatalf("read retained settlements: %v", err)
	}
	if err := queue.ackLease(t.Context(), "old"); err != nil {
		t.Fatalf("retry oldest settlement: %v", err)
	}
}

func recordExtendedLeaseSettlementHistory(t *testing.T, queue *DurableOrderQueue) error {
	t.Helper()

	err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := queue.recordLeaseSettlement(tx, "old", leaseSettlementAcknowledged); err != nil {
			return err
		}
		if err := queue.completedControlTargets.Put(tx, vault.Key("old"), leaseControlTarget{
			WorkerID: "worker",
			RunID:    "ab",
		}); err != nil {
			return fmt.Errorf("store completed control target: %w", err)
		}
		for index := range 4097 {
			if err := queue.recordLeaseSettlement(
				tx,
				fmt.Sprintf("new-%04d", index),
				leaseSettlementAcknowledged,
			); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("update extended lease settlement history: %w", err)
	}

	return nil
}

func assertExtendedLeaseSettlementHistory(t *testing.T, queue *DurableOrderQueue) error {
	t.Helper()

	err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		if record, found, err := queue.leaseSettlements.Get(tx, vault.Key("old")); err != nil ||
			!found || record.Outcome != leaseSettlementAcknowledged {
			t.Fatalf("old settlement = found %v, err %v", found, err)
		}
		if record, found, err := queue.leaseSettlements.Get(
			tx,
			vault.Key("new-4096"),
		); err != nil || !found || record.Sequence != 4097 {
			t.Fatalf("new settlement = %#v, found %v, err %v", record, found, err)
		}
		if _, found, err := queue.completedControlTargets.Get(
			tx,
			vault.Key("old"),
		); err != nil || !found {
			t.Fatalf("old completed owner = found %v, err %v", found, err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("view extended lease settlement history: %w", err)
	}

	return nil
}
