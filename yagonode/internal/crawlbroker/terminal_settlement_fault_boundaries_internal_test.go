package crawlbroker

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type terminalSettlementFaultFixture struct {
	storage scriptedQueueFixture
	leaseID string
	request terminalLeaseRequest
}

type terminalSettlementFaultSink struct {
	recordTerminal          func()
	confirmTerminalDelivery func()
}

func (terminalSettlementFaultSink) Record(
	context.Context,
	yagocrawlcontract.CrawlRunProgress,
) {
}

func (sink terminalSettlementFaultSink) RecordTerminal(
	context.Context,
	[]byte,
	yagocrawlcontract.CrawlRunProgress,
) error {
	if sink.recordTerminal != nil {
		sink.recordTerminal()
	}

	return nil
}

func (sink terminalSettlementFaultSink) ConfirmTerminalDelivery(
	context.Context,
	[]byte,
) error {
	if sink.confirmTerminalDelivery != nil {
		sink.confirmTerminalDelivery()
	}

	return nil
}

func newTerminalSettlementFaultFixture(
	t *testing.T,
	outcome leaseSettlementOutcome,
) terminalSettlementFaultFixture {
	t.Helper()
	storage := scriptedQueue(t)
	leaseID := leaseOneForSession(
		t,
		storage.queue,
		"terminal-fault",
		"worker",
		testWorkerSessionID,
	)

	return terminalSettlementFaultFixture{
		storage: storage,
		leaseID: leaseID,
		request: terminalRequestForLease(t, storage.queue, leaseID, outcome),
	}
}

func preparedTerminalSettlementFaultFixture(
	t *testing.T,
	outcome leaseSettlementOutcome,
) terminalSettlementFaultFixture {
	t.Helper()
	fixture := newTerminalSettlementFaultFixture(t, outcome)
	if _, err := fixture.storage.queue.prepareTerminalLeaseSettlement(
		t.Context(),
		fixture.leaseID,
		fixture.request,
	); err != nil {
		t.Fatalf("prepare terminal settlement: %v", err)
	}

	return fixture
}

func terminalRequestForLease(
	t *testing.T,
	queue *DurableOrderQueue,
	leaseID string,
	outcome leaseSettlementOutcome,
) terminalLeaseRequest {
	t.Helper()
	record, found := leaseRecordFor(t, queue, leaseID)
	if !found {
		t.Fatalf("lease %q is missing", leaseID)
	}
	identity := sha256.Sum256(record.OrderData)

	return terminalLeaseRequest{
		Outcome:         outcome,
		OrderIdentity:   identity[:],
		WorkerID:        record.WorkerID,
		WorkerSessionID: record.WorkerSessionID,
		State:           yagocrawlcontract.CrawlRunFinished,
	}
}

func replaceTerminalFaultLeaseOrder(
	t *testing.T,
	fixture terminalSettlementFaultFixture,
	orderData []byte,
) {
	t.Helper()
	record, found := leaseRecordFor(t, fixture.storage.queue, fixture.leaseID)
	if !found {
		t.Fatalf("lease %q is missing", fixture.leaseID)
	}
	record.OrderData = append([]byte(nil), orderData...)
	if err := fixture.storage.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return fixture.storage.queue.leases.Put(tx, vault.Key(fixture.leaseID), record)
	}); err != nil {
		t.Fatalf("replace terminal lease order: %v", err)
	}
}

func replaceTerminalFaultLeaseRecord(
	t *testing.T,
	fixture terminalSettlementFaultFixture,
	record leaseRecord,
) {
	t.Helper()
	if err := fixture.storage.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return fixture.storage.queue.leases.Put(tx, vault.Key(fixture.leaseID), record)
	}); err != nil {
		t.Fatalf("replace terminal lease record: %v", err)
	}
}

func requireTerminalPreparationFailure(t *testing.T, fixture terminalSettlementFaultFixture) {
	t.Helper()
	if _, err := fixture.storage.queue.prepareTerminalLeaseSettlement(
		t.Context(),
		fixture.leaseID,
		fixture.request,
	); err == nil {
		t.Fatal("terminal settlement preparation succeeded")
	}
}

func requireTerminalConfirmationFailure(t *testing.T, fixture terminalSettlementFaultFixture) {
	t.Helper()
	if err := fixture.storage.queue.confirmTerminalLeaseSettlement(
		t.Context(),
		fixture.leaseID,
		fixture.request,
	); err == nil {
		t.Fatal("terminal settlement confirmation succeeded")
	}
}

func TestTerminalSettlementPreparationRejectsLeaseFaults(t *testing.T) {
	t.Run("lease read", func(t *testing.T) {
		fixture := newTerminalSettlementFaultFixture(t, leaseSettlementAcknowledged)
		fixture.storage.engine.buckets[leaseBucket][fixture.leaseID] = []byte("{")
		requireTerminalPreparationFailure(t, fixture)
	})

	t.Run("lease owner", func(t *testing.T) {
		fixture := newTerminalSettlementFaultFixture(t, leaseSettlementAcknowledged)
		fixture.request.WorkerSessionID = "another-session"
		requireTerminalPreparationFailure(t, fixture)
	})

	t.Run("order identity", func(t *testing.T) {
		fixture := newTerminalSettlementFaultFixture(t, leaseSettlementAcknowledged)
		fixture.request.OrderIdentity[0] ^= 0xff
		requireTerminalPreparationFailure(t, fixture)
	})

	t.Run("order encoding", func(t *testing.T) {
		fixture := newTerminalSettlementFaultFixture(t, leaseSettlementAcknowledged)
		replaceTerminalFaultLeaseOrder(t, fixture, []byte("{"))
		fixture.request = terminalRequestForLease(
			t,
			fixture.storage.queue,
			fixture.leaseID,
			leaseSettlementAcknowledged,
		)
		requireTerminalPreparationFailure(t, fixture)
	})

	t.Run("order definition", func(t *testing.T) {
		fixture := newTerminalSettlementFaultFixture(t, leaseSettlementAcknowledged)
		order := testOrder("terminal-invalid-definition")
		order.Provenance = nil
		orderData, err := yagocrawlcontract.MarshalCrawlOrder(order)
		if err != nil {
			t.Fatalf("marshal terminal lease order: %v", err)
		}
		replaceTerminalFaultLeaseOrder(t, fixture, orderData)
		fixture.request = terminalRequestForLease(
			t,
			fixture.storage.queue,
			fixture.leaseID,
			leaseSettlementAcknowledged,
		)
		requireTerminalPreparationFailure(t, fixture)
	})
}

func TestTerminalSettlementPreparationSurfacesDispositionFaults(t *testing.T) {
	t.Run("control target write", func(t *testing.T) {
		fixture := newTerminalSettlementFaultFixture(t, leaseSettlementAcknowledged)
		fixture.storage.engine.putErrors[leaseControlTargetBucket] = errors.New("write failed")
		requireTerminalPreparationFailure(t, fixture)
	})

	t.Run("acknowledged lease delete", func(t *testing.T) {
		fixture := newTerminalSettlementFaultFixture(t, leaseSettlementAcknowledged)
		fixture.storage.engine.deleteErrors[leaseBucket] = errors.New("delete failed")
		requireTerminalPreparationFailure(t, fixture)
	})

	t.Run("requeued lease write", func(t *testing.T) {
		fixture := newTerminalSettlementFaultFixture(t, leaseSettlementRequeued)
		fixture.storage.engine.putErrors[leaseBucket] = errors.New("write failed")
		requireTerminalPreparationFailure(t, fixture)
	})
}

func TestTerminalSettlementConfirmationRejectsSettlementFaults(t *testing.T) {
	t.Run("settlement read", func(t *testing.T) {
		fixture := preparedTerminalSettlementFaultFixture(t, leaseSettlementAcknowledged)
		fixture.storage.engine.buckets[leaseSettlementBucket][fixture.leaseID] = []byte("{")
		requireTerminalConfirmationFailure(t, fixture)
	})

	t.Run("settlement identity", func(t *testing.T) {
		fixture := preparedTerminalSettlementFaultFixture(t, leaseSettlementAcknowledged)
		fixture.request.Tally.Fetched++
		requireTerminalConfirmationFailure(t, fixture)
	})
}

func TestTerminalSettlementConfirmationSurfacesRequeueFaults(t *testing.T) {
	t.Run("deferred lease read", func(t *testing.T) {
		fixture := preparedTerminalSettlementFaultFixture(t, leaseSettlementRequeued)
		fixture.storage.engine.buckets[leaseBucket][fixture.leaseID] = []byte("{")
		requireTerminalConfirmationFailure(t, fixture)
	})

	t.Run("deferred lease definition", func(t *testing.T) {
		fixture := preparedTerminalSettlementFaultFixture(t, leaseSettlementRequeued)
		record, found := leaseRecordFor(t, fixture.storage.queue, fixture.leaseID)
		if !found {
			t.Fatalf("lease %q is missing", fixture.leaseID)
		}
		record.Deferred = false
		replaceTerminalFaultLeaseRecord(t, fixture, record)
		requireTerminalConfirmationFailure(t, fixture)
	})

	t.Run("deferred lease delete", func(t *testing.T) {
		fixture := preparedTerminalSettlementFaultFixture(t, leaseSettlementRequeued)
		fixture.storage.engine.deleteErrors[leaseBucket] = errors.New("delete failed")
		requireTerminalConfirmationFailure(t, fixture)
	})

	t.Run("pending order write", func(t *testing.T) {
		fixture := preparedTerminalSettlementFaultFixture(t, leaseSettlementRequeued)
		fixture.storage.engine.putErrors[orderBucket] = errors.New("write failed")
		requireTerminalConfirmationFailure(t, fixture)
	})
}

func TestTerminalSettlementConfirmationSurfacesCleanupFaults(t *testing.T) {
	tests := []struct {
		name   string
		bucket vault.Name
	}{
		{name: "settlement delete", bucket: leaseSettlementBucket},
		{name: "settlement index delete", bucket: leaseSettlementOrderBucket},
		{name: "control target delete", bucket: leaseControlTargetBucket},
		{name: "completed control target delete", bucket: completedLeaseControlTargetBucket},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := preparedTerminalSettlementFaultFixture(t, leaseSettlementAcknowledged)
			if test.bucket == completedLeaseControlTargetBucket {
				if err := fixture.storage.queue.vault.Update(
					t.Context(),
					func(tx *vault.Txn) error {
						return fixture.storage.queue.completedControlTargets.Put(
							tx,
							vault.Key(fixture.leaseID),
							leaseControlTarget{WorkerID: "worker", RunID: testOrderRunID},
						)
					},
				); err != nil {
					t.Fatalf("store completed control target: %v", err)
				}
			}
			fixture.storage.engine.deleteErrors[test.bucket] = errors.New("delete failed")
			requireTerminalConfirmationFailure(t, fixture)
		})
	}
}

func TestTerminalProgressAcknowledgmentIsIdempotentAndSurfacesReadFailure(t *testing.T) {
	t.Run("idempotent", func(t *testing.T) {
		fixture := preparedTerminalSettlementFaultFixture(t, leaseSettlementAcknowledged)
		definition, err := fixture.storage.queue.prepareTerminalLeaseSettlement(
			t.Context(),
			fixture.leaseID,
			fixture.request,
		)
		if err != nil {
			t.Fatalf("read prepared terminal settlement: %v", err)
		}
		for attempt := 0; attempt < 2; attempt++ {
			if err := fixture.storage.queue.acknowledgeTerminalProgress(
				t.Context(),
				fixture.leaseID,
				definition,
			); err != nil {
				t.Fatalf("acknowledge terminal progress %d: %v", attempt, err)
			}
		}
	})

	t.Run("settlement read", func(t *testing.T) {
		fixture := preparedTerminalSettlementFaultFixture(t, leaseSettlementAcknowledged)
		definition := terminalSettlementRecord(fixture.request)
		fixture.storage.engine.buckets[leaseSettlementBucket][fixture.leaseID] = []byte("{")
		if err := fixture.storage.queue.acknowledgeTerminalProgress(
			t.Context(),
			fixture.leaseID,
			definition,
		); err == nil {
			t.Fatal("terminal progress acknowledgment succeeded")
		}
	})
}

func TestTerminalOrderSettlementSurfacesCrashWindowFaults(t *testing.T) {
	t.Run("confirmation", func(t *testing.T) {
		fixture := preparedTerminalSettlementFaultFixture(t, leaseSettlementAcknowledged)
		fixture.request.ConfirmationToken = terminalSettlementToken(
			fixture.storage.queue.terminalSettlementSecret,
			fixture.leaseID,
			fixture.request,
		)
		fixture.storage.engine.buckets[leaseSettlementBucket][fixture.leaseID] = []byte("{")
		server := newExchangeServer(fixture.storage.queue, nil)
		if _, err := server.settleTerminalOrder(
			t.Context(),
			fixture.leaseID,
			fixture.request,
		); err == nil {
			t.Fatal("terminal confirmation succeeded")
		}
	})

	t.Run("progress flag", func(t *testing.T) {
		fixture := newTerminalSettlementFaultFixture(t, leaseSettlementAcknowledged)
		server := newExchangeServer(fixture.storage.queue, nil)
		server.progress = terminalSettlementFaultSink{recordTerminal: func() {
			fixture.storage.engine.putErrors[leaseSettlementBucket] = errors.New("write failed")
		}}
		if _, err := server.settleTerminalOrder(
			t.Context(),
			fixture.leaseID,
			fixture.request,
		); err == nil {
			t.Fatal("terminal settlement succeeded")
		}
	})

	t.Run("order acknowledgment", func(t *testing.T) {
		fixture := newTerminalSettlementFaultFixture(t, leaseSettlementAcknowledged)
		server := newExchangeServer(fixture.storage.queue, nil)
		server.progress = terminalSettlementFaultSink{confirmTerminalDelivery: func() {
			fixture.storage.engine.buckets[leaseSettlementBucket][fixture.leaseID] = []byte("{")
		}}
		if _, err := server.settleTerminalOrder(
			t.Context(),
			fixture.leaseID,
			fixture.request,
		); err == nil {
			t.Fatal("terminal settlement succeeded")
		}
	})
}
