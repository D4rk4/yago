package crawlbroker

import (
	"errors"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
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
	owned, err := fixture.queue.terminalProgressOwnedBy(t.Context(), "worker", testOrderRunID)
	if err != nil || !owned {
		t.Fatalf("completed owner = %v err=%v, want worker", owned, err)
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

func TestRunLeaseOwnershipClassifiesDurableLease(t *testing.T) {
	queue := memQueue(t)
	if ownership, err := queue.runLeaseOwnership(
		t.Context(),
		"",
		testOrderRunID,
	); err != nil ||
		ownership != runLeaseUnclaimed {
		t.Fatalf("empty worker ownership = %v err=%v", ownership, err)
	}
	leaseID := leaseOne(t, queue, "ownership", "worker")
	for _, test := range []struct {
		workerID string
		runID    string
		want     runLeaseOwnership
	}{
		{workerID: "worker", runID: testOrderRunID, want: runLeaseOwnedByWorker},
		{workerID: "other", runID: testOrderRunID, want: runLeaseOwnedByAnotherWorker},
		{workerID: "worker", runID: "ab", want: runLeaseUnclaimed},
	} {
		ownership, err := queue.runLeaseOwnership(t.Context(), test.workerID, test.runID)
		if err != nil || ownership != test.want {
			t.Fatalf(
				"ownership %q/%q = %v err=%v, want %v",
				test.workerID,
				test.runID,
				ownership,
				err,
				test.want,
			)
		}
	}
	if err := queue.deferLease(t.Context(), leaseID); err != nil {
		t.Fatalf("defer lease: %v", err)
	}
	if ownership, err := queue.runLeaseOwnership(
		t.Context(),
		"worker",
		testOrderRunID,
	); err != nil ||
		ownership != runLeaseUnclaimed {
		t.Fatalf("deferred ownership = %v err=%v", ownership, err)
	}
}

func TestRunLeaseOwnershipSurfacesStorageFailures(t *testing.T) {
	t.Run("scan", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.scanErrors[leaseBucket] = errors.New("scan failed")
		if _, err := fixture.queue.runLeaseOwnership(t.Context(), "worker", "ab"); err == nil {
			t.Fatal("lease scan failure was hidden")
		}
	})
	t.Run("order decode", func(t *testing.T) {
		fixture := scriptedQueue(t)
		leaseID := leaseOne(t, fixture.queue, "corrupt", "worker")
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			record, _, err := fixture.queue.leases.Get(tx, vault.Key(leaseID))
			if err != nil {
				return fmt.Errorf("read lease for corrupt order: %w", err)
			}
			record.OrderData = []byte("{")

			if err := fixture.queue.leases.Put(tx, vault.Key(leaseID), record); err != nil {
				return fmt.Errorf("store corrupt lease order: %w", err)
			}

			return nil
		}); err != nil {
			t.Fatalf("corrupt lease order: %v", err)
		}
		if _, err := fixture.queue.runLeaseOwnership(t.Context(), "worker", "ab"); err == nil {
			t.Fatal("lease order decode failure was hidden")
		}
	})
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

func TestPersistentRunReassignmentEdges(t *testing.T) {
	ledger, engine := persistentControlLedgerFixture(t)
	queue, err := newDurableOrderQueue(ledger.storage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("open queue: %v", err)
	}
	if ownership, err := ledger.ReassignRunIfLeaseOwned(
		t.Context(),
		queue,
		"worker",
		"",
	); err != nil ||
		ownership != runLeaseUnclaimed {
		t.Fatalf("empty run reassignment = %v err=%v", ownership, err)
	}
	if ownership, err := ledger.ReassignRunIfLeaseOwned(
		t.Context(),
		queue,
		"worker",
		"ab",
	); err != nil ||
		ownership != runLeaseUnclaimed {
		t.Fatalf("missing run reassignment = %v err=%v", ownership, err)
	}
	if err := ledger.ReconcileRun(t.Context(), "worker", "missing", false); err != nil {
		t.Fatalf("missing run reconciliation: %v", err)
	}
	if _, err := ledger.Enqueue(
		t.Context(),
		"worker",
		yagocrawlcontract.CrawlControlDirective{RunID: "ab"},
	); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	otherQueue := memQueue(t)
	if _, err := ledger.ReassignRunIfLeaseOwned(
		t.Context(),
		otherQueue,
		"worker",
		"ab",
	); err == nil {
		t.Fatal("storage identity mismatch was accepted")
	}
	engine.scanErrors[leaseBucket] = errors.New("scan failed")
	if _, err := ledger.ReassignRunIfLeaseOwned(t.Context(), queue, "worker", "ab"); err == nil {
		t.Fatal("lease scan failure was hidden")
	}
}

func TestPersistentRunReassignmentSurfacesDirectiveLookupFailure(t *testing.T) {
	ledger, engine := persistentControlLedgerFixture(t)
	queue, err := newDurableOrderQueue(ledger.storage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("open queue: %v", err)
	}
	engine.scanErrors[controlDirectiveBucket] = errors.New("scan failed")
	if _, err := ledger.ReassignRunIfLeaseOwned(t.Context(), queue, "worker", "ab"); err == nil {
		t.Fatal("directive lookup failure was hidden")
	}
}

func TestMemoryRunReassignmentWithoutLeaseIsIgnored(t *testing.T) {
	registry := newControlRegistry()
	ownership, err := registry.ReassignRunIfLeaseOwned(
		t.Context(),
		memQueue(t),
		"worker",
		"ab",
	)
	if err != nil || ownership != runLeaseUnclaimed {
		t.Fatalf("unleased reassignment = %v err=%v", ownership, err)
	}
}

func TestPersistentRunReassignmentSurfacesAtomicDirectiveScanFailure(t *testing.T) {
	ledger, engine := persistentControlLedgerFixture(t)
	queue, err := newDurableOrderQueue(ledger.storage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("open queue: %v", err)
	}
	leaseOne(t, queue, "atomic-scan", "worker")
	if _, err := ledger.Enqueue(
		t.Context(),
		"old",
		yagocrawlcontract.CrawlControlDirective{RunID: testOrderRunID},
	); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	engine.replayNext = true
	engine.betweenReplay = func() {
		engine.scanErrors[controlDirectiveBucket] = errors.New("scan failed")
	}
	if _, err := ledger.ReassignRunIfLeaseOwned(
		t.Context(),
		queue,
		"worker",
		testOrderRunID,
	); err == nil {
		t.Fatal("atomic directive scan failure was hidden")
	}
}

func TestTerminalProgressOwnershipUsesPendingAcknowledgment(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "pending-terminal", "worker")
	if _, err := queue.ackLeaseWithTarget(t.Context(), leaseID); err != nil {
		t.Fatalf("ack lease: %v", err)
	}
	for _, test := range []struct {
		workerID string
		want     bool
	}{
		{workerID: "worker", want: true},
		{workerID: "stale", want: false},
	} {
		owned, err := queue.terminalProgressOwnedBy(t.Context(), test.workerID, testOrderRunID)
		if err != nil || owned != test.want {
			t.Fatalf("pending owner %q = %v err=%v, want %v", test.workerID, owned, err, test.want)
		}
	}
}

func TestTerminalProgressOwnershipRejectsMissingIdentity(t *testing.T) {
	queue := memQueue(t)
	for _, identity := range [][2]string{{"", "ab"}, {"worker", ""}} {
		owned, err := queue.terminalProgressOwnedBy(t.Context(), identity[0], identity[1])
		if err != nil || owned {
			t.Fatalf("empty identity %+v = %v err=%v", identity, owned, err)
		}
	}
	owned, err := queue.terminalProgressOwnedBy(t.Context(), "worker", "ab")
	if err != nil || owned {
		t.Fatalf("unknown terminal owner = %v err=%v", owned, err)
	}
}

func TestTerminalProgressOwnershipSurfacesStorageFailures(t *testing.T) {
	t.Run("lease order", func(t *testing.T) {
		fixture := scriptedQueue(t)
		leaseID := leaseOne(t, fixture.queue, "terminal-order", "worker")
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			record, _, err := fixture.queue.leases.Get(tx, vault.Key(leaseID))
			if err != nil {
				return fmt.Errorf("read lease for corrupt terminal order: %w", err)
			}
			record.OrderData = []byte("{")

			if err := fixture.queue.leases.Put(tx, vault.Key(leaseID), record); err != nil {
				return fmt.Errorf("store corrupt terminal order: %w", err)
			}

			return nil
		}); err != nil {
			t.Fatalf("corrupt lease order: %v", err)
		}
		if _, err := fixture.queue.terminalProgressOwnedBy(
			t.Context(),
			"worker",
			"ab",
		); err == nil {
			t.Fatal("lease order failure was hidden")
		}
	})
	t.Run("pending target scan", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.scanErrors[leaseControlTargetBucket] = errors.New("scan failed")
		if _, err := fixture.queue.terminalProgressOwnedBy(
			t.Context(),
			"worker",
			"ab",
		); err == nil {
			t.Fatal("pending target scan failure was hidden")
		}
	})
	t.Run("completed target scan", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.scanErrors[completedLeaseControlTargetBucket] = errors.New("scan failed")
		if _, err := fixture.queue.terminalProgressOwnedBy(
			t.Context(),
			"worker",
			"ab",
		); err == nil {
			t.Fatal("completed target scan failure was hidden")
		}
	})
}

func TestControlTargetOwnershipRejectsAmbiguousHistory(t *testing.T) {
	queue := memQueue(t)
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := queue.completedControlTargets.Put(tx, vault.Key("a"), leaseControlTarget{
			WorkerID: "worker-a",
			RunID:    "ab",
		}); err != nil {
			return fmt.Errorf("store first completed control target: %w", err)
		}
		if err := queue.completedControlTargets.Put(tx, vault.Key("b"), leaseControlTarget{
			WorkerID: "worker-b",
			RunID:    "ab",
		}); err != nil {
			return fmt.Errorf("store second completed control target: %w", err)
		}

		if err := queue.completedControlTargets.Put(tx, vault.Key("0-other"), leaseControlTarget{
			WorkerID: "worker-a",
			RunID:    "cd",
		}); err != nil {
			return fmt.Errorf("store unrelated completed control target: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("prepare completed owners: %v", err)
	}
	owned, err := queue.terminalProgressOwnedBy(t.Context(), "worker-a", "ab")
	if err != nil || owned {
		t.Fatalf("ambiguous completed owner = %v err=%v", owned, err)
	}
}
