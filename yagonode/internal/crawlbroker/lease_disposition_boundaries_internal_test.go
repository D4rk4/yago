package crawlbroker

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestRunControlCompletionRejectsUnreadableOrConflictingSettlement(t *testing.T) {
	t.Run("unreadable", func(t *testing.T) {
		fixture := scriptedQueue(t)
		putLeaseControlTarget(t, fixture.queue, leaseControlTarget{
			WorkerID: "worker",
			RunID:    testOrderRunID,
		})
		fixture.engine.buckets[leaseSettlementBucket]["lease"] = []byte("{")
		if err := fixture.queue.completeRunControl(t.Context(), "lease"); err == nil {
			t.Fatal("unreadable settlement was accepted")
		}
	})

	t.Run("conflicting", func(t *testing.T) {
		fixture := scriptedQueue(t)
		putLeaseControlTarget(t, fixture.queue, leaseControlTarget{
			WorkerID: "worker",
			RunID:    testOrderRunID,
		})
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.recordLeaseSettlement(tx, "lease", leaseSettlementRequeued)
		}); err != nil {
			t.Fatalf("record conflicting settlement: %v", err)
		}
		if err := fixture.queue.completeRunControl(
			t.Context(),
			"lease",
		); !errors.Is(err, errLeaseDispositionConflict) {
			t.Fatalf("conflicting completion error = %v", err)
		}
	})
}

func TestPendingRunControlIsNotReassignedWithoutOwnedLease(t *testing.T) {
	queue := memQueue(t)
	ledger, err := newPersistentControlDirectiveLedger(queue.vault)
	if err != nil {
		t.Fatalf("open control ledger: %v", err)
	}
	if _, err := ledger.Enqueue(t.Context(), "previous", yagocrawlcontract.CrawlControlDirective{
		RunID: testOrderRunID,
	}); err != nil {
		t.Fatalf("enqueue control: %v", err)
	}
	ownership, err := ledger.ReassignRunIfLeaseOwned(
		t.Context(),
		queue,
		"worker",
		testOrderRunID,
	)
	if err != nil || ownership != runLeaseUnclaimed {
		t.Fatalf("ownership=%v error=%v", ownership, err)
	}
}

func TestLeaseDispositionRejectsWrongOwnerAndPreservesControlFailures(t *testing.T) {
	t.Run("defer wrong owner", func(t *testing.T) {
		queue := memQueue(t)
		leaseID := leaseOneForSession(t, queue, "wrong-defer", "worker", testWorkerSessionID)
		if err := queue.deferLeaseForOwner(
			t.Context(),
			leaseID,
			"worker",
			"other-session",
		); !errors.Is(err, errLeaseLost) {
			t.Fatalf("wrong-owner defer error = %v", err)
		}
	})

	t.Run("ack wrong owner", func(t *testing.T) {
		queue := memQueue(t)
		leaseID := leaseOneForSession(t, queue, "wrong-ack", "worker", testWorkerSessionID)
		if _, err := queue.ackLeaseWithOwner(
			t.Context(),
			leaseID,
			"worker",
			"other-session",
		); !errors.Is(err, errLeaseLost) {
			t.Fatalf("wrong-owner ack error = %v", err)
		}
	})

	t.Run("generic ack failure", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.buckets[leaseSettlementBucket]["lease"] = []byte("{")
		server := newExchangeServer(fixture.queue, nil)
		if err := server.acknowledgeOrder(t.Context(), "lease"); err == nil {
			t.Fatal("generic acknowledgment failure was hidden")
		}
	})

	t.Run("owner ack without run", func(t *testing.T) {
		queue := memQueue(t)
		order := testOrder("owner-no-run")
		order.Provenance = nil
		if err := queue.Publish(t.Context(), order); err != nil {
			t.Fatalf("publish order: %v", err)
		}
		_, leaseID, found, err := queue.leasePopForSession(
			t.Context(),
			"worker",
			testWorkerSessionID,
		)
		if err != nil || !found {
			t.Fatalf("lease found=%v error=%v", found, err)
		}
		server := newExchangeServer(queue, nil)
		if err := server.acknowledgeOrderForOwner(
			t.Context(),
			leaseID,
			"worker",
			testWorkerSessionID,
		); err != nil {
			t.Fatalf("owner acknowledgment: %v", err)
		}
	})

	t.Run("owner control completion", func(t *testing.T) {
		queue := memQueue(t)
		leaseID := leaseOneForSession(t, queue, "owner-control", "worker", testWorkerSessionID)
		ledger := &scriptedControlDirectiveLedger{reconcileErr: errors.New("control failed")}
		server := newExchangeServer(queue, nil)
		server.control = newControlRegistryWithLedger(ledger)
		if err := server.acknowledgeOrderForOwner(
			t.Context(),
			leaseID,
			"worker",
			testWorkerSessionID,
		); err == nil {
			t.Fatal("owner control completion failure was hidden")
		}
	})
}

func TestLeaseSweepAndClaimSurfaceInterleavingFailures(t *testing.T) {
	t.Run("retention", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.buckets[seqBucket][string(leaseSettlementNextKey)] = []byte{1}
		if err := fixture.queue.sweepExpired(t.Context()); err == nil {
			t.Fatal("retention failure was hidden")
		}
	})

	t.Run("cancel after scan", func(t *testing.T) {
		queue := memQueue(t)
		leaseOne(t, queue, "cancel-requeue", "worker")
		ctx, cancel := context.WithCancel(t.Context())
		err := queue.requeueLeasesMatching(ctx, func(leaseRecord) bool {
			cancel()

			return true
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled requeue error = %v", err)
		}
	})

	t.Run("capacity scan", func(t *testing.T) {
		fixture := scriptedQueue(t)
		if err := fixture.queue.Publish(t.Context(), testOrder("claim-capacity")); err != nil {
			t.Fatalf("publish order: %v", err)
		}
		fixture.engine.buckets[leaseBucket]["corrupt"] = []byte("{")
		if _, _, err := fixture.queue.claimPendingOrder(
			t.Context(),
			"new-lease",
			"worker",
			testWorkerSessionID,
		); err == nil {
			t.Fatal("capacity scan failure was hidden")
		}
	})

	t.Run("requeue read and disappearance", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.buckets[leaseBucket]["corrupt"] = []byte("{")
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			_, err := fixture.queue.requeueLeaseTx(
				tx,
				vault.Key("corrupt"),
				func(leaseRecord) bool { return true },
			)

			return err
		}); err == nil {
			t.Fatal("requeue read failure was hidden")
		}
		delete(fixture.engine.buckets[leaseBucket], "corrupt")
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			changed, err := fixture.queue.requeueLeaseTx(
				tx,
				vault.Key("missing"),
				func(leaseRecord) bool { return true },
			)
			if err != nil || changed {
				t.Fatalf("missing requeue changed=%v error=%v", changed, err)
			}

			return nil
		}); err != nil {
			t.Fatalf("missing requeue: %v", err)
		}
	})
}

func TestTerminalOwnershipRejectsAnotherWorker(t *testing.T) {
	queue := memQueue(t)
	leaseOne(t, queue, "another-owner", "other-worker")
	owned, err := queue.terminalProgressOwnedBy(t.Context(), "worker", testOrderRunID)
	if err != nil || owned {
		t.Fatalf("owned=%v error=%v", owned, err)
	}
}
