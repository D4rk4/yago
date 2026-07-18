package crawlbroker

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestFinalizedTerminalAcknowledgmentExpiresAtFixedHorizon(t *testing.T) {
	set := withClock(t)
	base := time.Unix(130_000, 0)
	set(base)
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "terminal-ack-retention", "worker")
	request := terminalOrderAcknowledgment(t, queue, leaseID, "worker", false)
	server := newExchangeServer(queue, nil)
	token := terminalRetentionToken(t, server, request)
	assertFinalizedTerminalSettlement(t, queue, leaseID, base)
	assertLeaseSettlementRetentionCounts(t, queue, leaseSettlementRows{1, 1, 1, 1, 0})

	set(base.Add(leaseSettlementRetention - time.Nanosecond))
	if err := queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("sweep terminal acknowledgment before horizon: %v", err)
	}
	assertLeaseSettlementRetentionCounts(t, queue, leaseSettlementRows{1, 1, 1, 1, 0})

	set(base.Add(leaseSettlementRetention))
	if err := queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("sweep terminal acknowledgment at horizon: %v", err)
	}
	assertLeaseSettlementRetentionCounts(t, queue, leaseSettlementRows{})
	confirmExpiredTerminalSettlement(t, server, request, token)
}

func TestFinalizedTerminalRequeueExpiresAndRequeuesAtomically(t *testing.T) {
	set := withClock(t)
	base := time.Unix(140_000, 0)
	set(base)
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "terminal-requeue-retention", "worker")
	request := terminalOrderAcknowledgment(t, queue, leaseID, "worker", true)
	request.TerminalState = crawlrpc.CrawlRunState_CRAWL_RUN_STATE_CANCELLED
	server := newExchangeServer(queue, nil)
	token := terminalRetentionToken(t, server, request)
	assertFinalizedTerminalSettlement(t, queue, leaseID, base)

	set(base.Add(leaseSettlementRetention - time.Nanosecond))
	if err := queue.deleteExpiredLeaseSettlements(t.Context(), nowFunc()); err != nil {
		t.Fatalf("retain terminal requeue before horizon: %v", err)
	}
	if record, found := leaseRecordFor(t, queue, leaseID); !found || !record.Deferred {
		t.Fatalf("deferred terminal requeue = %+v found=%v", record, found)
	}

	set(base.Add(leaseSettlementRetention))
	if err := queue.deleteExpiredLeaseSettlements(t.Context(), nowFunc()); err != nil {
		t.Fatalf("expire terminal requeue at horizon: %v", err)
	}
	depth, err := queue.Depth(t.Context())
	if err != nil || depth != (QueueDepth{Pending: 1}) {
		t.Fatalf("expired terminal requeue depth = %+v error=%v", depth, err)
	}
	assertLeaseSettlementRetentionCounts(t, queue, leaseSettlementRows{})
	confirmExpiredTerminalSettlement(t, server, request, token)
	depth, err = queue.Depth(t.Context())
	if err != nil || depth != (QueueDepth{Pending: 1}) {
		t.Fatalf("late-confirmed terminal requeue depth = %+v error=%v", depth, err)
	}
}

func TestIncompleteTerminalSettlementNeverEntersRetentionIndex(t *testing.T) {
	set := withClock(t)
	base := time.Unix(150_000, 0)
	for _, delivered := range []bool{false, true} {
		t.Run(fmt.Sprintf("progress-delivered-%t", delivered), func(t *testing.T) {
			assertIncompleteTerminalSettlementRetention(t, set, base, delivered)
		})
	}
}

func assertIncompleteTerminalSettlementRetention(
	t *testing.T,
	set func(time.Time),
	base time.Time,
	delivered bool,
) {
	t.Helper()
	set(base)
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, fmt.Sprintf("incomplete-%t", delivered), "worker")
	protoRequest := terminalOrderAcknowledgment(t, queue, leaseID, "worker", false)
	request, rich, err := terminalLeaseRequestFromProto(protoRequest)
	if err != nil || !rich {
		t.Fatalf("decode terminal request: rich=%v error=%v", rich, err)
	}
	settlement, err := queue.prepareTerminalLeaseSettlement(t.Context(), leaseID, request)
	if err != nil {
		t.Fatalf("prepare incomplete terminal settlement: %v", err)
	}
	if delivered {
		if err := queue.acknowledgeTerminalProgress(
			t.Context(),
			leaseID,
			settlement,
		); err != nil {
			t.Fatalf("acknowledge terminal progress: %v", err)
		}
	}
	set(base.Add(leaseSettlementRetention))
	if err := queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("sweep incomplete terminal settlement: %v", err)
	}
	assertLeaseSettlementRetentionCounts(
		t,
		queue,
		leaseSettlementRows{settlements: 1, orderEntries: 1, pendingControls: 1},
	)
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		record, found, err := queue.leaseSettlements.Get(tx, vault.Key(leaseID))
		if err != nil || !found || record.FinalizedAtUnixNano != 0 ||
			record.ProgressDelivered != delivered {
			t.Fatalf("incomplete settlement = %+v found=%v error=%v", record, found, err)
		}

		return nil
	}); err != nil {
		t.Fatalf("read incomplete terminal settlement: %v", err)
	}
}

func TestTerminalAcknowledgmentFinalizationRequiresCompletedRunControl(t *testing.T) {
	set := withClock(t)
	base := time.Unix(160_000, 0)
	set(base)
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "terminal-finalization-control", "worker")
	protoRequest := terminalOrderAcknowledgment(t, queue, leaseID, "worker", false)
	request, rich, err := terminalLeaseRequestFromProto(protoRequest)
	if err != nil || !rich {
		t.Fatalf("decode terminal request: rich=%v error=%v", rich, err)
	}
	settlement, err := queue.prepareTerminalLeaseSettlement(t.Context(), leaseID, request)
	if err != nil {
		t.Fatalf("prepare terminal settlement: %v", err)
	}
	if err := queue.acknowledgeTerminalProgress(t.Context(), leaseID, settlement); err != nil {
		t.Fatalf("acknowledge terminal progress: %v", err)
	}
	if err := queue.finalizeTerminalLeaseSettlement(
		t.Context(),
		leaseID,
		settlement,
	); !errors.Is(err, errLeaseDispositionConflict) {
		t.Fatalf("finalize before run control completion = %v", err)
	}
	if err := queue.completeRunControl(t.Context(), leaseID); err != nil {
		t.Fatalf("complete terminal run control: %v", err)
	}
	if err := queue.finalizeTerminalLeaseSettlement(t.Context(), leaseID, settlement); err != nil {
		t.Fatalf("finalize after run control completion: %v", err)
	}
	if err := queue.finalizeTerminalLeaseSettlement(t.Context(), leaseID, settlement); err != nil {
		t.Fatalf("repeat terminal finalization: %v", err)
	}
	assertFinalizedTerminalSettlement(t, queue, leaseID, base)
}

func TestTerminalSettlementRetentionSurvivesBrokerRestart(t *testing.T) {
	set := withClock(t)
	base := time.Unix(170_000, 0)
	set(base)
	path := filepath.Join(t.TempDir(), "node.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open first terminal retention storage: %v", err)
	}
	first, err := newDurableOrderQueue(storage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("open first terminal retention queue: %v", err)
	}
	leaseID := leaseOne(t, first, "terminal-retention-restart", "worker")
	request := terminalOrderAcknowledgment(t, first, leaseID, "worker", false)
	firstServer := newExchangeServer(first, nil)
	token := terminalRetentionToken(t, firstServer, request)
	if err := storage.Close(); err != nil {
		t.Fatalf("close first terminal retention storage: %v", err)
	}

	set(base.Add(leaseSettlementRetention))
	storage, err = boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen terminal retention storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	second, err := newDurableOrderQueue(storage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("open second terminal retention queue: %v", err)
	}
	if err := second.sweepExpired(t.Context()); err != nil {
		t.Fatalf("expire terminal settlement after restart: %v", err)
	}
	assertLeaseSettlementRetentionCounts(t, second, leaseSettlementRows{})
	confirmExpiredTerminalSettlement(t, newExchangeServer(second, nil), request, token)
}

func TestTerminalSettlementRetentionSweepIsBounded(t *testing.T) {
	set := withClock(t)
	base := time.Unix(180_000, 0)
	set(base)
	queue := memQueue(t)
	const settlements = maximumLeaseSettlementRetentionChunk + 1
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		for index := range settlements {
			record := validTerminalSettlementRecord()
			record.ProgressDelivered = true
			record.FinalizedAtUnixNano = base.UnixNano()
			leaseID := fmt.Sprintf("terminal-retention-%03d", index)
			stored, err := queue.recordTerminalLeaseSettlement(tx, leaseID, record)
			if err != nil {
				return err
			}
			if err := queue.leaseSettlementExpiry.Put(
				tx,
				leaseSettlementExpiryKey(stored),
				[]byte(leaseID),
			); err != nil {
				return fmt.Errorf("store bounded terminal expiry: %w", err)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("record bounded terminal settlements: %v", err)
	}
	set(base.Add(leaseSettlementRetention))
	if err := queue.deleteExpiredLeaseSettlements(t.Context(), nowFunc()); err != nil {
		t.Fatalf("first bounded terminal sweep: %v", err)
	}
	assertLeaseSettlementRetentionCounts(t, queue, leaseSettlementRows{1, 1, 1, 0, 0})
	if err := queue.deleteExpiredLeaseSettlements(t.Context(), nowFunc()); err != nil {
		t.Fatalf("second bounded terminal sweep: %v", err)
	}
	assertLeaseSettlementRetentionCounts(t, queue, leaseSettlementRows{})
}

func TestTerminalRequeueRetentionRollsBackAtomically(t *testing.T) {
	set := withClock(t)
	base := time.Unix(190_000, 0)
	set(base)
	fixture := scriptedQueue(t)
	leaseID := leaseOne(t, fixture.queue, "terminal-retention-rollback", "worker")
	request := terminalOrderAcknowledgment(t, fixture.queue, leaseID, "worker", true)
	request.TerminalState = crawlrpc.CrawlRunState_CRAWL_RUN_STATE_CANCELLED
	terminalRetentionToken(t, newExchangeServer(fixture.queue, nil), request)
	set(base.Add(leaseSettlementRetention))
	fixture.engine.putErrors[orderBucket] = errors.New("write failed")
	if err := fixture.queue.deleteExpiredLeaseSettlements(t.Context(), nowFunc()); err == nil {
		t.Fatal("terminal requeue retention failure was hidden")
	}
	if record, found := leaseRecordFor(t, fixture.queue, leaseID); !found || !record.Deferred {
		t.Fatalf("rolled-back deferred lease = %+v found=%v", record, found)
	}
	assertLeaseSettlementRetentionCounts(t, fixture.queue, leaseSettlementRows{1, 1, 1, 0, 0})
	delete(fixture.engine.putErrors, orderBucket)
	if err := fixture.queue.deleteExpiredLeaseSettlements(t.Context(), nowFunc()); err != nil {
		t.Fatalf("retry terminal requeue retention: %v", err)
	}
	depth, err := fixture.queue.Depth(t.Context())
	if err != nil || depth != (QueueDepth{Pending: 1}) {
		t.Fatalf("retried terminal requeue depth = %+v error=%v", depth, err)
	}
}

func terminalRetentionToken(
	t *testing.T,
	server *exchangeServer,
	request *crawlrpc.OrderAck,
) []byte {
	t.Helper()
	result, err := server.AckOrder(t.Context(), request)
	if err != nil {
		t.Fatalf("finalize terminal settlement: %v", err)
	}
	if len(result.GetConfirmationToken()) != sha256.Size {
		t.Fatalf("terminal settlement token length = %d", len(result.GetConfirmationToken()))
	}

	return append([]byte(nil), result.GetConfirmationToken()...)
}

func confirmExpiredTerminalSettlement(
	t *testing.T,
	server *exchangeServer,
	request *crawlrpc.OrderAck,
	token []byte,
) {
	t.Helper()
	confirmation := copyTerminalOrderAcknowledgment(request)
	confirmation.ConfirmationToken = append([]byte(nil), token...)
	result, err := server.AckOrder(t.Context(), confirmation)
	if err != nil || len(result.GetConfirmationToken()) != 0 {
		t.Fatalf("late terminal settlement confirmation = %+v error=%v", result, err)
	}
}

func assertFinalizedTerminalSettlement(
	t *testing.T,
	queue *DurableOrderQueue,
	leaseID string,
	finalizedAt time.Time,
) {
	t.Helper()
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		record, found, err := queue.leaseSettlements.Get(tx, vault.Key(leaseID))
		if err != nil || !found || !record.Terminal || !record.ProgressDelivered ||
			record.FinalizedAtUnixNano != finalizedAt.UnixNano() {
			t.Fatalf("finalized terminal settlement = %+v found=%v error=%v", record, found, err)
		}
		indexedLeaseID, indexed, err := queue.leaseSettlementExpiry.Get(
			tx,
			leaseSettlementExpiryKey(record),
		)
		if err != nil || !indexed || !bytes.Equal(indexedLeaseID, []byte(leaseID)) {
			t.Fatalf(
				"terminal settlement expiry = %q found=%v error=%v",
				indexedLeaseID,
				indexed,
				err,
			)
		}

		return nil
	}); err != nil {
		t.Fatalf("read finalized terminal settlement: %v", err)
	}
}

func TestTerminalSettlementCodecRejectsInvalidFinalization(t *testing.T) {
	record := validTerminalSettlementRecord()
	record.FinalizedAtUnixNano = 1
	if err := validateLeaseSettlementRecord(record); err == nil {
		t.Fatal("terminal settlement finalized before progress delivery")
	}
	record.ProgressDelivered = true
	record.FinalizedAtUnixNano = -1
	if err := validateLeaseSettlementRecord(record); err == nil {
		t.Fatal("terminal settlement with negative finalization time validated")
	}
	if err := validateLeaseSettlementRecord(leaseSettlementRecord{
		Outcome:             leaseSettlementAcknowledged,
		FinalizedAtUnixNano: 1,
	}); err == nil {
		t.Fatal("legacy settlement with terminal finalization validated")
	}
}

func TestTerminalSettlementRetentionRejectsMismatchedDeferredLease(t *testing.T) {
	set := withClock(t)
	base := time.Unix(200_000, 0)
	set(base)
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "terminal-retention-mismatch", "worker")
	request := terminalOrderAcknowledgment(t, queue, leaseID, "worker", true)
	request.TerminalState = crawlrpc.CrawlRunState_CRAWL_RUN_STATE_CANCELLED
	terminalRetentionToken(t, newExchangeServer(queue, nil), request)
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		record, found, err := queue.leases.Get(tx, vault.Key(leaseID))
		if err != nil || !found {
			return fmt.Errorf("read deferred terminal lease: found=%v error=%w", found, err)
		}
		record.WorkerID = "replacement"

		return queue.leases.Put(tx, vault.Key(leaseID), record)
	}); err != nil {
		t.Fatalf("replace deferred terminal lease: %v", err)
	}
	set(base.Add(leaseSettlementRetention))
	if err := queue.deleteExpiredLeaseSettlements(
		t.Context(),
		nowFunc(),
	); !errors.Is(err, errLeaseDispositionConflict) {
		t.Fatalf("mismatched deferred terminal lease expiry = %v", err)
	}
}

func TestTerminalSettlementFinalizationRejectsWrongDefinition(t *testing.T) {
	fixture := preparedTerminalSettlementFaultFixture(t, leaseSettlementRequeued)
	if err := fixture.storage.queue.acknowledgeTerminalProgress(
		t.Context(),
		fixture.leaseID,
		terminalSettlementRecord(fixture.request),
	); err != nil {
		t.Fatalf("acknowledge terminal progress: %v", err)
	}
	definition := terminalSettlementRecord(fixture.request)
	definition.Progress.Tally.Fetched++
	if err := fixture.storage.queue.finalizeTerminalLeaseSettlement(
		t.Context(),
		fixture.leaseID,
		definition,
	); !errors.Is(err, errLeaseDispositionConflict) {
		t.Fatalf("wrong terminal finalization definition = %v", err)
	}
}

func TestTerminalSettlementFinalizationRejectsNonpositiveClock(t *testing.T) {
	set := withClock(t)
	set(time.Unix(0, 0))
	fixture := preparedTerminalSettlementFaultFixture(t, leaseSettlementRequeued)
	definition, err := fixture.storage.queue.prepareTerminalLeaseSettlement(
		t.Context(),
		fixture.leaseID,
		fixture.request,
	)
	if err != nil {
		t.Fatalf("read prepared terminal settlement: %v", err)
	}
	if err := fixture.storage.queue.acknowledgeTerminalProgress(
		t.Context(),
		fixture.leaseID,
		definition,
	); err != nil {
		t.Fatalf("acknowledge terminal progress: %v", err)
	}
	if err := fixture.storage.queue.finalizeTerminalLeaseSettlement(
		t.Context(),
		fixture.leaseID,
		definition,
	); err == nil {
		t.Fatal("terminal settlement finalized with nonpositive clock")
	}
}

func TestTerminalSettlementFinalizationSurfacesStorageFaults(t *testing.T) {
	t.Run("incomplete progress", func(t *testing.T) {
		fixture := preparedTerminalSettlementFaultFixture(t, leaseSettlementRequeued)
		definition := terminalSettlementRecord(fixture.request)
		if err := fixture.storage.queue.finalizeTerminalLeaseSettlement(
			t.Context(),
			fixture.leaseID,
			definition,
		); !errors.Is(err, errLeaseDispositionConflict) {
			t.Fatalf("incomplete terminal finalization = %v", err)
		}
	})
	for _, test := range []struct {
		name    string
		prepare func(*testing.T, *terminalSettlementFaultFixture)
	}{
		{
			name: "settlement write",
			prepare: func(_ *testing.T, fixture *terminalSettlementFaultFixture) {
				fixture.storage.engine.putErrors[leaseSettlementBucket] = errors.New("write failed")
			},
		},
		{
			name: "expiry write",
			prepare: func(_ *testing.T, fixture *terminalSettlementFaultFixture) {
				fixture.storage.engine.putErrors[leaseSettlementExpiryBucket] = errors.New("write failed")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, definition := finalizableTerminalSettlementFaultFixture(
				t,
				leaseSettlementRequeued,
			)
			test.prepare(t, &fixture)
			if err := fixture.storage.queue.finalizeTerminalLeaseSettlement(
				t.Context(),
				fixture.leaseID,
				definition,
			); err == nil {
				t.Fatal("terminal finalization storage failure was hidden")
			}
		})
	}
}

func TestTerminalAcknowledgmentFinalizationSurfacesControlFaults(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*testing.T, *terminalSettlementFaultFixture)
	}{
		{
			name: "pending target read",
			prepare: func(_ *testing.T, fixture *terminalSettlementFaultFixture) {
				fixture.storage.engine.buckets[leaseControlTargetBucket][fixture.leaseID] = []byte("{")
			},
		},
		{
			name: "completed target read",
			prepare: func(_ *testing.T, fixture *terminalSettlementFaultFixture) {
				fixture.storage.engine.buckets[completedLeaseControlTargetBucket][fixture.leaseID] = []byte("{")
			},
		},
		{
			name: "completed target mismatch",
			prepare: func(t *testing.T, fixture *terminalSettlementFaultFixture) {
				if err := fixture.storage.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
					return fixture.storage.queue.completedControlTargets.Put(
						tx,
						vault.Key(fixture.leaseID),
						leaseControlTarget{WorkerID: "another", RunID: testOrderRunID},
					)
				}); err != nil {
					t.Fatalf("replace completed terminal target: %v", err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, definition := finalizableTerminalSettlementFaultFixture(
				t,
				leaseSettlementAcknowledged,
			)
			test.prepare(t, &fixture)
			if err := fixture.storage.queue.finalizeTerminalLeaseSettlement(
				t.Context(),
				fixture.leaseID,
				definition,
			); err == nil {
				t.Fatal("terminal control finalization failure was hidden")
			}
		})
	}
}

func TestTerminalOrderSettlementSurfacesFinalizationFault(t *testing.T) {
	fixture := newTerminalSettlementFaultFixture(t, leaseSettlementRequeued)
	server := newExchangeServer(fixture.storage.queue, nil)
	server.progress = terminalSettlementFaultSink{confirmTerminalDelivery: func() {
		fixture.storage.engine.putErrors[leaseSettlementExpiryBucket] = errors.New("write failed")
	}}
	if _, err := server.settleTerminalOrder(
		t.Context(),
		fixture.leaseID,
		fixture.request,
	); err == nil {
		t.Fatal("terminal order finalization failure was hidden")
	}
}

func TestTerminalSettlementConfirmationSurfacesExpiryCleanupFault(t *testing.T) {
	set := withClock(t)
	set(time.Unix(210_000, 0))
	fixture := newTerminalSettlementFaultFixture(t, leaseSettlementAcknowledged)
	server := newExchangeServer(fixture.storage.queue, nil)
	token, err := server.settleTerminalOrder(t.Context(), fixture.leaseID, fixture.request)
	if err != nil {
		t.Fatalf("finalize terminal settlement: %v", err)
	}
	fixture.storage.engine.deleteErrors[leaseSettlementExpiryBucket] = errors.New("delete failed")
	fixture.request.ConfirmationToken = token
	if _, err := server.settleTerminalOrder(
		t.Context(),
		fixture.leaseID,
		fixture.request,
	); err == nil {
		t.Fatal("terminal expiry cleanup failure was hidden")
	}
}

func TestTerminalSettlementExpirySurfacesTerminalFaults(t *testing.T) {
	set := withClock(t)
	base := time.Unix(220_000, 0)
	set(base)
	for _, test := range []struct {
		name    string
		prepare func(*terminalSettlementFaultFixture)
	}{
		{
			name: "settlement index",
			prepare: func(fixture *terminalSettlementFaultFixture) {
				fixture.storage.engine.buckets[leaseSettlementOrderBucket][string(orderKey(0))] = []byte{0}
			},
		},
		{
			name: "terminal cleanup",
			prepare: func(fixture *terminalSettlementFaultFixture) {
				fixture.storage.engine.deleteErrors[leaseSettlementBucket] = errors.New("delete failed")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			set(base)
			fixture := newTerminalSettlementFaultFixture(t, leaseSettlementAcknowledged)
			server := newExchangeServer(fixture.storage.queue, nil)
			if _, err := server.settleTerminalOrder(
				t.Context(),
				fixture.leaseID,
				fixture.request,
			); err != nil {
				t.Fatalf("finalize terminal settlement: %v", err)
			}
			test.prepare(&fixture)
			set(base.Add(leaseSettlementRetention))
			if err := fixture.storage.queue.deleteExpiredLeaseSettlements(
				t.Context(),
				nowFunc(),
			); err == nil {
				t.Fatal("terminal expiry fault was hidden")
			}
		})
	}
}

func TestNoopProgressSinkRecordsOrdinaryProgress(t *testing.T) {
	noopProgressSink{}.Record(t.Context(), yagocrawlcontract.CrawlRunProgress{})
}

func finalizableTerminalSettlementFaultFixture(
	t *testing.T,
	outcome leaseSettlementOutcome,
) (terminalSettlementFaultFixture, leaseSettlementRecord) {
	t.Helper()
	fixture := preparedTerminalSettlementFaultFixture(t, outcome)
	definition, err := fixture.storage.queue.prepareTerminalLeaseSettlement(
		t.Context(),
		fixture.leaseID,
		fixture.request,
	)
	if err != nil {
		t.Fatalf("read prepared terminal settlement: %v", err)
	}
	if err := fixture.storage.queue.acknowledgeTerminalProgress(
		t.Context(),
		fixture.leaseID,
		definition,
	); err != nil {
		t.Fatalf("acknowledge terminal progress: %v", err)
	}
	if outcome == leaseSettlementAcknowledged {
		if err := fixture.storage.queue.completeRunControl(
			t.Context(),
			fixture.leaseID,
		); err != nil {
			t.Fatalf("complete terminal control: %v", err)
		}
	}

	return fixture, definition
}
