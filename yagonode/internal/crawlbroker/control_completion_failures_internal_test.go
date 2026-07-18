package crawlbroker

import (
	"errors"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func putLeaseControlTarget(
	t *testing.T,
	queue *DurableOrderQueue,
	target leaseControlTarget,
) {
	t.Helper()
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := queue.leaseControlTargets.Put(tx, vault.Key("lease"), target); err != nil {
			return fmt.Errorf("store lease control target: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("put lease control target: %v", err)
	}
}

func TestControlCompletionReplaySurfacesStorageFailures(t *testing.T) {
	t.Run("outbox scan", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.scanErrors[leaseControlTargetBucket] = errors.New("scan failed")
		if err := fixture.queue.replayRunControlCompletions(
			t.Context(),
			newControlRegistry(),
		); err == nil {
			t.Fatal("outbox scan failure was hidden")
		}
	})
	t.Run("directive cleanup", func(t *testing.T) {
		fixture := scriptedQueue(t)
		putLeaseControlTarget(t, fixture.queue, leaseControlTarget{
			WorkerID: "worker",
			RunID:    "ab",
		})
		ledger := &scriptedControlDirectiveLedger{reconcileErr: errors.New("cleanup failed")}
		if err := fixture.queue.replayRunControlCompletions(
			t.Context(),
			newControlRegistryWithLedger(ledger),
		); err == nil {
			t.Fatal("directive cleanup failure was hidden")
		}
	})
	t.Run("completed owner write", func(t *testing.T) {
		fixture := scriptedQueue(t)
		putLeaseControlTarget(t, fixture.queue, leaseControlTarget{
			WorkerID: "worker",
			RunID:    "ab",
		})
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.recordLeaseSettlement(
				tx,
				"lease",
				leaseSettlementAcknowledged,
			)
		}); err != nil {
			t.Fatalf("record settlement: %v", err)
		}
		fixture.engine.putErrors[completedLeaseControlTargetBucket] = errors.New("put failed")
		if err := fixture.queue.replayRunControlCompletions(
			t.Context(),
			newControlRegistry(),
		); err == nil {
			t.Fatal("completed owner write failure was hidden")
		}
	})
	t.Run("outbox delete", func(t *testing.T) {
		fixture := scriptedQueue(t)
		putLeaseControlTarget(t, fixture.queue, leaseControlTarget{
			WorkerID: "worker",
			RunID:    "ab",
		})
		fixture.engine.deleteErrors[leaseControlTargetBucket] = errors.New("delete failed")
		if err := fixture.queue.replayRunControlCompletions(
			t.Context(),
			newControlRegistry(),
		); err == nil {
			t.Fatal("outbox delete failure was hidden")
		}
	})
	t.Run("outbox read", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.buckets[leaseControlTargetBucket]["lease"] = []byte("{")
		if err := fixture.queue.completeRunControl(t.Context(), "lease"); err == nil {
			t.Fatal("outbox read failure was hidden")
		}
	})
	t.Run("already complete", func(t *testing.T) {
		if err := memQueue(t).completeRunControl(t.Context(), "missing"); err != nil {
			t.Fatalf("missing completion target: %v", err)
		}
	})
}

func TestAcknowledgmentRetriesDurableControlCleanup(t *testing.T) {
	fixture := scriptedQueue(t)
	leaseID := leaseOne(t, fixture.queue, "retry-cleanup", "worker")
	ledger := &scriptedControlDirectiveLedger{reconcileErr: errors.New("cleanup failed")}
	server := newExchangeServer(fixture.queue, nil)
	server.control = newControlRegistryWithLedger(ledger)
	if err := server.acknowledgeOrder(t.Context(), leaseID); err == nil {
		t.Fatal("control cleanup failure was hidden")
	}
	pending, err := fixture.queue.pendingRunControlCompletions(t.Context())
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending cleanup = %+v err=%v, want one", pending, err)
	}
	ledger.reconcileErr = nil
	if err := server.acknowledgeOrder(t.Context(), leaseID); err != nil {
		t.Fatalf("retry cleanup: %v", err)
	}
	pending, err = fixture.queue.pendingRunControlCompletions(t.Context())
	if err != nil || len(pending) != 0 {
		t.Fatalf("completed cleanup = %+v err=%v, want empty", pending, err)
	}
}

func TestAcknowledgmentWithoutRunControlTarget(t *testing.T) {
	queue := memQueue(t)
	order := testOrder("no-provenance")
	order.Provenance = nil
	if err := queue.Publish(t.Context(), order); err != nil {
		t.Fatalf("publish: %v", err)
	}
	_, leaseID, found, err := queue.leasePop(t.Context(), "worker")
	if err != nil || !found {
		t.Fatalf("lease: found=%v err=%v", found, err)
	}
	server := newExchangeServer(queue, nil)
	if err := server.acknowledgeOrder(t.Context(), leaseID); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	if err := queue.ackLease(t.Context(), leaseID); err != nil {
		t.Fatalf("duplicate acknowledgment: %v", err)
	}
}

func TestLeaseControlTargetCodecRejectsCorruption(t *testing.T) {
	for _, raw := range [][]byte{[]byte("{"), []byte(`{"worker":"","run":""}`)} {
		if _, err := (leaseControlTargetCodec{}).Decode(raw); err == nil {
			t.Fatalf("decoded corrupt target %q", raw)
		}
	}
}

func TestControlTargetRejectsMalformedLeaseOrder(t *testing.T) {
	if _, err := controlTargetFromLease(leaseRecord{OrderData: []byte("{")}); err == nil {
		t.Fatal("malformed lease order produced a control target")
	}
}

func TestAckLeaseControlTargetFailures(t *testing.T) {
	t.Run("target write", func(t *testing.T) {
		fixture := scriptedQueue(t)
		leaseID := leaseOne(t, fixture.queue, "target-write", "worker")
		fixture.engine.putErrors[leaseControlTargetBucket] = errors.New("put failed")
		if _, err := fixture.queue.ackLeaseWithTarget(t.Context(), leaseID); err == nil {
			t.Fatal("target write failure was hidden")
		}
	})
	t.Run("target retry read", func(t *testing.T) {
		fixture := scriptedQueue(t)
		leaseID := leaseOne(t, fixture.queue, "target-read", "worker")
		if _, err := fixture.queue.ackLeaseWithTarget(t.Context(), leaseID); err != nil {
			t.Fatalf("first ack: %v", err)
		}
		fixture.engine.buckets[leaseControlTargetBucket][leaseID] = []byte("{")
		if _, err := fixture.queue.ackLeaseWithTarget(t.Context(), leaseID); err == nil {
			t.Fatal("target retry read failure was hidden")
		}
	})
	t.Run("lease order decode", func(t *testing.T) {
		fixture := scriptedQueue(t)
		leaseID := leaseOne(t, fixture.queue, "order-decode", "worker")
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			record, _, err := fixture.queue.leases.Get(tx, vault.Key(leaseID))
			if err != nil {
				return fmt.Errorf("read lease for corrupt target order: %w", err)
			}
			record.OrderData = []byte("{")

			if err := fixture.queue.leases.Put(tx, vault.Key(leaseID), record); err != nil {
				return fmt.Errorf("store corrupt target order: %w", err)
			}

			return nil
		}); err != nil {
			t.Fatalf("corrupt lease order: %v", err)
		}
		if _, err := fixture.queue.ackLeaseWithTarget(t.Context(), leaseID); err == nil {
			t.Fatal("lease order decode failure was hidden")
		}
	})
}
