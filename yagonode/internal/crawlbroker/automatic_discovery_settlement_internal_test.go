package crawlbroker

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func automaticDiscoverySettlementLease(
	t *testing.T,
	queue *DurableOrderQueue,
	target string,
) ([]byte, string) {
	t.Helper()
	requireAutomaticDiscoveryAdmission(t, queue, target, false)
	data, leaseID, found, err := queue.leasePopForSession(
		t.Context(),
		"worker",
		"session",
	)
	if err != nil || !found {
		t.Fatalf("lease automatic discovery = %t, %v", found, err)
	}

	return data, leaseID
}

func automaticDiscoveryTerminalRequest(data []byte) terminalLeaseRequest {
	identity := sha256.Sum256(data)

	return terminalLeaseRequest{
		Outcome:         leaseSettlementAcknowledged,
		OrderIdentity:   identity[:],
		WorkerID:        "worker",
		WorkerSessionID: "session",
		State:           yagocrawlcontract.CrawlRunFinished,
		Tally:           yagocrawlcontract.CrawlRunTally{Failed: 1},
	}
}

func automaticDiscoverySettlementIntentFor(
	target string,
	sequence uint64,
) automaticDiscoverySettlementIntent {
	return automaticDiscoverySettlementIntent{
		Lease: leaseRecord{
			DiscoveryKey:      target,
			DiscoverySequence: sequence,
		},
		Settlement: leaseSettlementRecord{Outcome: leaseSettlementAcknowledged},
	}
}

type automaticDiscoverySettlementStateExpectation struct {
	target   string
	leaseID  string
	active   bool
	settling bool
}

func requireAutomaticDiscoverySettlementState(
	t *testing.T,
	queue *DurableOrderQueue,
	expected automaticDiscoverySettlementStateExpectation,
) {
	t.Helper()
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, activeFound, err := queue.activeDiscoveryKeys.Get(tx, vault.Key(expected.target))
		if err != nil {
			return fmt.Errorf("read active discovery ownership: %w", err)
		}
		_, settlementFound, err := queue.discoverySettlements.Get(
			tx,
			vault.Key(expected.leaseID),
		)
		if err != nil {
			return fmt.Errorf("read automatic discovery settlement: %w", err)
		}
		if activeFound != expected.active || settlementFound != expected.settling {
			return fmt.Errorf(
				"automatic discovery state = active %t, settling %t; want active %t, settling %t",
				activeFound,
				settlementFound,
				expected.active,
				expected.settling,
			)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAutomaticDiscoverySettlementNormalPaths(t *testing.T) {
	target := "https://settlement-normal.example/"
	t.Run("acknowledgment", func(t *testing.T) {
		fixture := scriptedQueue(t)
		_, leaseID := automaticDiscoverySettlementLease(t, fixture.queue, target)
		if _, err := fixture.queue.ackLeaseWithOwner(
			t.Context(),
			leaseID,
			"worker",
			"session",
		); err != nil {
			t.Fatalf("acknowledge automatic discovery: %v", err)
		}
		requireAutomaticDiscoverySettlementState(
			t,
			fixture.queue,
			automaticDiscoverySettlementStateExpectation{
				target:  target,
				leaseID: leaseID,
			},
		)
	})
	t.Run("terminal", func(t *testing.T) {
		fixture := scriptedQueue(t)
		data, leaseID := automaticDiscoverySettlementLease(t, fixture.queue, target)
		if _, err := fixture.queue.prepareTerminalLeaseSettlement(
			t.Context(),
			leaseID,
			automaticDiscoveryTerminalRequest(data),
		); err != nil {
			t.Fatalf("settle terminal automatic discovery: %v", err)
		}
		requireAutomaticDiscoverySettlementState(
			t,
			fixture.queue,
			automaticDiscoverySettlementStateExpectation{
				target:  target,
				leaseID: leaseID,
			},
		)
	})
}

func TestAutomaticDiscoverySettlementRetriesConverge(t *testing.T) {
	target := "https://settlement-retry.example/"
	t.Run("acknowledgment", func(t *testing.T) {
		fixture := scriptedQueue(t)
		_, leaseID := automaticDiscoverySettlementLease(t, fixture.queue, target)
		fixture.engine.deleteErrors[activeDiscoveryKeyBucket] = errors.New("delete failed")
		if _, err := fixture.queue.ackLeaseWithOwner(
			t.Context(),
			leaseID,
			"worker",
			"session",
		); err == nil {
			t.Fatal("active ownership release failure was hidden")
		}
		requireAutomaticDiscoverySettlementState(
			t,
			fixture.queue,
			automaticDiscoverySettlementStateExpectation{
				target:   target,
				leaseID:  leaseID,
				active:   true,
				settling: true,
			},
		)
		delete(fixture.engine.deleteErrors, activeDiscoveryKeyBucket)
		if _, err := fixture.queue.ackLeaseWithOwner(
			t.Context(),
			leaseID,
			"worker",
			"session",
		); err != nil {
			t.Fatalf("retry automatic discovery acknowledgment: %v", err)
		}
		requireAutomaticDiscoverySettlementState(
			t,
			fixture.queue,
			automaticDiscoverySettlementStateExpectation{
				target:  target,
				leaseID: leaseID,
			},
		)
	})
	t.Run("terminal", func(t *testing.T) {
		fixture := scriptedQueue(t)
		data, leaseID := automaticDiscoverySettlementLease(t, fixture.queue, target)
		request := automaticDiscoveryTerminalRequest(data)
		fixture.engine.deleteErrors[activeDiscoveryKeyBucket] = errors.New("delete failed")
		if _, err := fixture.queue.prepareTerminalLeaseSettlement(
			t.Context(),
			leaseID,
			request,
		); err == nil {
			t.Fatal("terminal active ownership release failure was hidden")
		}
		requireAutomaticDiscoverySettlementState(
			t,
			fixture.queue,
			automaticDiscoverySettlementStateExpectation{
				target:   target,
				leaseID:  leaseID,
				active:   true,
				settling: true,
			},
		)
		delete(fixture.engine.deleteErrors, activeDiscoveryKeyBucket)
		if _, err := fixture.queue.prepareTerminalLeaseSettlement(
			t.Context(),
			leaseID,
			request,
		); err != nil {
			t.Fatalf("retry terminal automatic discovery settlement: %v", err)
		}
		requireAutomaticDiscoverySettlementState(
			t,
			fixture.queue,
			automaticDiscoverySettlementStateExpectation{
				target:  target,
				leaseID: leaseID,
			},
		)
	})
}

func TestAutomaticDiscoverySettlementReopenConverges(t *testing.T) {
	target := "https://settlement-reopen.example/"
	for _, terminal := range []bool{false, true} {
		name := "acknowledgment"
		if terminal {
			name = "terminal"
		}
		t.Run(name, func(t *testing.T) {
			fixture := scriptedQueue(t)
			data, leaseID := automaticDiscoverySettlementLease(t, fixture.queue, target)
			fixture.engine.deleteErrors[activeDiscoveryKeyBucket] = errors.New("delete failed")
			if terminal {
				if _, err := fixture.queue.prepareTerminalLeaseSettlement(
					t.Context(),
					leaseID,
					automaticDiscoveryTerminalRequest(data),
				); err == nil {
					t.Fatal("terminal release failure was hidden")
				}
			} else {
				if _, err := fixture.queue.ackLeaseWithOwner(
					t.Context(),
					leaseID,
					"worker",
					"session",
				); err == nil {
					t.Fatal("acknowledgment release failure was hidden")
				}
			}
			requireAutomaticDiscoverySettlementState(
				t,
				fixture.queue,
				automaticDiscoverySettlementStateExpectation{
					target:   target,
					leaseID:  leaseID,
					active:   true,
					settling: true,
				},
			)
			delete(fixture.engine.deleteErrors, activeDiscoveryKeyBucket)
			reopened := reopenAutomaticDiscoveryQueue(t, fixture.engine)
			requireAutomaticDiscoverySettlementState(
				t,
				reopened,
				automaticDiscoverySettlementStateExpectation{
					target:  target,
					leaseID: leaseID,
				},
			)
		})
	}
}

func TestAutomaticDiscoverySettlementFaultsPropagate(t *testing.T) {
	target := "https://settlement-fault.example/"
	testAutomaticDiscoverySettlementPersistenceFaults(t, target)
	testAutomaticDiscoverySettlementConvergenceFaults(t, target)
	testAutomaticDiscoverySettlementConflictFaults(t, target)
}

func testAutomaticDiscoverySettlementPersistenceFaults(t *testing.T, target string) {
	t.Helper()
	t.Run("persist", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.putErrors[discoverySettlementBucket] = errors.New("write failed")
		err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.persistAutomaticDiscoverySettlementTx(
				tx,
				"lease",
				automaticDiscoverySettlementIntentFor(target, 0),
			)
		})
		if err == nil {
			t.Fatal("settlement persistence failure was hidden")
		}
	})
	t.Run("acknowledgment persistence", func(t *testing.T) {
		fixture := scriptedQueue(t)
		_, leaseID := automaticDiscoverySettlementLease(t, fixture.queue, target)
		fixture.engine.putErrors[discoverySettlementBucket] = errors.New("write failed")
		if _, err := fixture.queue.ackLeaseWithOwner(
			t.Context(),
			leaseID,
			"worker",
			"session",
		); err == nil {
			t.Fatal("acknowledgment settlement failure was hidden")
		}
		requireAutomaticDiscoverySettlementState(
			t,
			fixture.queue,
			automaticDiscoverySettlementStateExpectation{
				target:  target,
				leaseID: leaseID,
				active:  true,
			},
		)
	})
	t.Run("terminal persistence", func(t *testing.T) {
		fixture := scriptedQueue(t)
		data, leaseID := automaticDiscoverySettlementLease(t, fixture.queue, target)
		fixture.engine.putErrors[discoverySettlementBucket] = errors.New("write failed")
		if _, err := fixture.queue.prepareTerminalLeaseSettlement(
			t.Context(),
			leaseID,
			automaticDiscoveryTerminalRequest(data),
		); err == nil {
			t.Fatal("terminal settlement persistence failure was hidden")
		}
		requireAutomaticDiscoverySettlementState(
			t,
			fixture.queue,
			automaticDiscoverySettlementStateExpectation{
				target:  target,
				leaseID: leaseID,
				active:  true,
			},
		)
	})
}

func testAutomaticDiscoverySettlementConvergenceFaults(t *testing.T, target string) {
	t.Helper()
	t.Run("read", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.readErrors[discoverySettlementBucket] = errors.New("read failed")
		if _, err := fixture.queue.completeAutomaticDiscoverySettlement(
			t.Context(),
			"lease",
		); err == nil {
			t.Fatal("settlement read failure was hidden")
		}
	})
	t.Run("lease read", func(t *testing.T) {
		fixture := automaticDiscoverySettlementFixture(t, target, true)
		fixture.engine.readErrors[leaseBucket] = errors.New("read failed")
		if _, err := fixture.queue.completeAutomaticDiscoverySettlement(
			t.Context(),
			"lease",
		); err == nil {
			t.Fatal("settlement lease read failure was hidden")
		}
	})
	t.Run("live lease recovery", func(t *testing.T) {
		fixture := automaticDiscoverySettlementFixture(t, target, true)
		if _, err := fixture.queue.completeAutomaticDiscoverySettlement(
			t.Context(),
			"lease",
		); err != nil {
			t.Fatalf("recover live settlement: %v", err)
		}
		requireAutomaticDiscoverySettlementState(
			t,
			fixture.queue,
			automaticDiscoverySettlementStateExpectation{
				target:  target,
				leaseID: "lease",
			},
		)
	})
	t.Run("disposition read", func(t *testing.T) {
		fixture := automaticDiscoverySettlementFixture(t, target, false)
		fixture.engine.readErrors[leaseSettlementBucket] = errors.New("read failed")
		if _, err := fixture.queue.completeAutomaticDiscoverySettlement(
			t.Context(),
			"lease",
		); err == nil {
			t.Fatal("settlement disposition read failure was hidden")
		}
	})
}

func testAutomaticDiscoverySettlementConflictFaults(t *testing.T, target string) {
	t.Helper()
	t.Run("release", func(t *testing.T) {
		fixture := automaticDiscoverySettlementFixture(t, target, false)
		fixture.engine.deleteErrors[discoverySettlementBucket] = errors.New("delete failed")
		if _, err := fixture.queue.completeAutomaticDiscoverySettlement(
			t.Context(),
			"lease",
		); err == nil {
			t.Fatal("settlement release failure was hidden")
		}
		delete(fixture.engine.deleteErrors, discoverySettlementBucket)
		if _, err := fixture.queue.completeAutomaticDiscoverySettlement(
			t.Context(),
			"lease",
		); err != nil {
			t.Fatalf("retry settlement release: %v", err)
		}
	})
	t.Run("requeued", func(t *testing.T) {
		fixture := automaticDiscoverySettlementFixture(t, target, false)
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.recordLeaseSettlement(
				tx,
				"lease",
				leaseSettlementRequeued,
			)
		}); err != nil {
			t.Fatalf("record requeued settlement: %v", err)
		}
		if _, err := fixture.queue.completeAutomaticDiscoverySettlement(
			t.Context(),
			"lease",
		); err == nil {
			t.Fatal("conflicting requeued settlement was accepted")
		}
		requireAutomaticDiscoverySettlementState(
			t,
			fixture.queue,
			automaticDiscoverySettlementStateExpectation{
				target:   target,
				leaseID:  "lease",
				active:   true,
				settling: true,
			},
		)
	})
	t.Run("legacy release helper", func(t *testing.T) {
		fixture := scriptedQueue(t)
		if err := fixture.queue.releaseActiveAutomaticDiscovery(
			t.Context(),
			leaseRecord{},
		); err != nil {
			t.Fatalf("release empty automatic discovery: %v", err)
		}
		if err := fixture.queue.releaseActiveAutomaticDiscovery(
			t.Context(),
			leaseRecord{DiscoveryKey: target},
		); err != nil {
			t.Fatalf("release absent automatic discovery: %v", err)
		}
	})
}

func automaticDiscoverySettlementFixture(
	t *testing.T,
	target string,
	leased bool,
) scriptedQueueFixture {
	t.Helper()
	fixture := scriptedQueue(t)
	if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := fixture.queue.activeDiscoveryKeys.Put(tx, vault.Key(target), 7); err != nil {
			return fmt.Errorf("seed active discovery ownership: %w", err)
		}
		record := leaseRecord{DiscoveryKey: target, DiscoverySequence: 7}
		if err := fixture.queue.persistAutomaticDiscoverySettlementTx(
			tx,
			"lease",
			automaticDiscoverySettlementIntentFor(target, 7),
		); err != nil {
			return err
		}
		if leased {
			if err := fixture.queue.leases.Put(tx, vault.Key("lease"), record); err != nil {
				return fmt.Errorf("seed automatic discovery lease: %w", err)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("seed automatic discovery settlement: %v", err)
	}

	return fixture
}

func TestAutomaticDiscoverySettlementStartupFailuresPropagate(t *testing.T) {
	t.Run("register", func(t *testing.T) {
		engine := newScriptedEngine()
		engine.provisionErrors[discoverySettlementBucket] = errors.New("provision failed")
		storage, err := vault.New(engine)
		if err != nil {
			t.Fatalf("open settlement storage: %v", err)
		}
		if _, err := newDurableOrderQueue(storage, DefaultLeaseTTL); err == nil {
			t.Fatal("settlement collection registration failure was hidden")
		}
	})
	t.Run("scan", func(t *testing.T) {
		engine := newScriptedEngine()
		storage, err := vault.New(engine)
		if err != nil {
			t.Fatalf("open settlement storage: %v", err)
		}
		engine.scanErrors[discoverySettlementBucket] = errors.New("scan failed")
		if _, err := newDurableOrderQueue(storage, DefaultLeaseTTL); err == nil {
			t.Fatal("settlement scan failure was hidden")
		}
	})
	t.Run("completion", func(t *testing.T) {
		fixture := automaticDiscoverySettlementFixture(
			t,
			"https://settlement-startup.example/",
			false,
		)
		fixture.engine.readErrors[discoverySettlementBucket] = errors.New("read failed")
		if err := fixture.queue.reconcileAutomaticDiscoverySettlements(
			t.Context(),
		); err == nil {
			t.Fatal("settlement reconciliation failure was hidden")
		}
	})
}
