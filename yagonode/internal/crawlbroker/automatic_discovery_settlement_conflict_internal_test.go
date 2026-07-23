package crawlbroker

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func automaticDiscoverySettlementIntentBytes(
	t *testing.T,
	engine *scriptedEngine,
	leaseID string,
) []byte {
	t.Helper()
	raw := engine.buckets[discoverySettlementBucket][leaseID]
	if len(raw) == 0 {
		t.Fatal("automatic discovery settlement intent is missing")
	}

	return append([]byte(nil), raw...)
}

func requireAutomaticDiscoverySettlementIntentUnchanged(
	t *testing.T,
	engine *scriptedEngine,
	leaseID string,
	want []byte,
) {
	t.Helper()
	if got := engine.buckets[discoverySettlementBucket][leaseID]; !bytes.Equal(got, want) {
		t.Fatal("automatic discovery settlement intent changed")
	}
}

func removeAutomaticDiscoverySettlementLease(
	t *testing.T,
	queue *DurableOrderQueue,
	leaseID string,
) {
	t.Helper()
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		_, err := queue.leases.Delete(tx, vault.Key(leaseID))
		if err != nil {
			return fmt.Errorf("delete automatic discovery lease: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("remove staged settlement lease: %v", err)
	}
}

func TestAutomaticDiscoveryTerminalSettlementIntentIsImmutable(t *testing.T) {
	fixture := scriptedQueue(t)
	target := "https://immutable-terminal-settlement.example/"
	data, leaseID := automaticDiscoverySettlementLease(t, fixture.queue, target)
	request := automaticDiscoveryTerminalRequest(data)
	if err := fixture.queue.stageAutomaticDiscoveryTerminalSettlement(
		t.Context(),
		leaseID,
		request,
	); err != nil {
		t.Fatalf("stage terminal settlement: %v", err)
	}
	staged := automaticDiscoverySettlementIntentBytes(t, fixture.engine, leaseID)
	if err := fixture.queue.stageAutomaticDiscoveryTerminalSettlement(
		t.Context(),
		leaseID,
		request,
	); err != nil {
		t.Fatalf("retry identical terminal settlement: %v", err)
	}
	requireAutomaticDiscoverySettlementIntentUnchanged(t, fixture.engine, leaseID, staged)

	differentTally := request
	differentTally.Tally.Failed++
	if err := fixture.queue.stageAutomaticDiscoveryTerminalSettlement(
		t.Context(),
		leaseID,
		differentTally,
	); !errors.Is(err, errLeaseDispositionConflict) {
		t.Fatalf("different terminal tally error = %v", err)
	}
	requireAutomaticDiscoverySettlementIntentUnchanged(t, fixture.engine, leaseID, staged)

	differentIdentity := request
	differentIdentity.OrderIdentity = append([]byte(nil), request.OrderIdentity...)
	differentIdentity.OrderIdentity[0] ^= 0xff
	if err := fixture.queue.stageAutomaticDiscoveryTerminalSettlement(
		t.Context(),
		leaseID,
		differentIdentity,
	); !errors.Is(err, errLeaseDispositionConflict) {
		t.Fatalf("different terminal identity error = %v", err)
	}
	requireAutomaticDiscoverySettlementIntentUnchanged(t, fixture.engine, leaseID, staged)

	if err := fixture.queue.stageAutomaticDiscoveryAcknowledgment(
		t.Context(),
		leaseID,
		"worker",
		"session",
		true,
	); !errors.Is(err, errLeaseDispositionConflict) {
		t.Fatalf("simple acknowledgment conflict error = %v", err)
	}
	requireAutomaticDiscoverySettlementIntentUnchanged(t, fixture.engine, leaseID, staged)

	removeAutomaticDiscoverySettlementLease(t, fixture.queue, leaseID)
	requireRetainedAutomaticDiscoveryTerminalSettlementIntent(
		t,
		fixture,
		leaseID,
		request,
		staged,
	)
}

func requireRetainedAutomaticDiscoveryTerminalSettlementIntent(
	t *testing.T,
	fixture scriptedQueueFixture,
	leaseID string,
	request terminalLeaseRequest,
	staged []byte,
) {
	t.Helper()
	if err := fixture.queue.stageAutomaticDiscoveryTerminalSettlement(
		t.Context(),
		leaseID,
		request,
	); err != nil {
		t.Fatalf("retry identical terminal settlement without lease: %v", err)
	}
	differentTally := request
	differentTally.Tally.Failed++
	if err := fixture.queue.stageAutomaticDiscoveryTerminalSettlement(
		t.Context(),
		leaseID,
		differentTally,
	); !errors.Is(err, errLeaseDispositionConflict) {
		t.Fatalf("different retained terminal tally error = %v", err)
	}
	differentWorker := request
	differentWorker.WorkerID = "other"
	if err := fixture.queue.stageAutomaticDiscoveryTerminalSettlement(
		t.Context(),
		leaseID,
		differentWorker,
	); !errors.Is(err, errLeaseLost) {
		t.Fatalf("different retained terminal worker error = %v", err)
	}
	if err := fixture.queue.stageAutomaticDiscoveryAcknowledgment(
		t.Context(),
		leaseID,
		"worker",
		"session",
		true,
	); !errors.Is(err, errLeaseDispositionConflict) {
		t.Fatalf("retained simple acknowledgment conflict error = %v", err)
	}
	requeue := request
	requeue.Outcome = leaseSettlementRequeued
	if err := fixture.queue.stageAutomaticDiscoveryTerminalSettlement(
		t.Context(),
		leaseID,
		requeue,
	); !errors.Is(err, errLeaseDispositionConflict) {
		t.Fatalf("retained requeue conflict error = %v", err)
	}
	requireAutomaticDiscoverySettlementIntentUnchanged(t, fixture.engine, leaseID, staged)
}

func TestAutomaticDiscoveryAcknowledgmentIntentRejectsTerminalReplacement(t *testing.T) {
	fixture := scriptedQueue(t)
	target := "https://immutable-acknowledgment-settlement.example/"
	data, leaseID := automaticDiscoverySettlementLease(t, fixture.queue, target)
	if err := fixture.queue.stageAutomaticDiscoveryAcknowledgment(
		t.Context(),
		leaseID,
		"worker",
		"session",
		true,
	); err != nil {
		t.Fatalf("stage acknowledgment: %v", err)
	}
	staged := automaticDiscoverySettlementIntentBytes(t, fixture.engine, leaseID)
	if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		record, found, err := fixture.queue.leases.Get(tx, vault.Key(leaseID))
		if err != nil {
			return fmt.Errorf("read staged automatic discovery lease: %w", err)
		}
		if !found {
			return errors.New("staged lease is missing")
		}
		record.ExpiresAtUnixNano += int64(time.Minute)

		if err := fixture.queue.leases.Put(tx, vault.Key(leaseID), record); err != nil {
			return fmt.Errorf("extend staged automatic discovery lease: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("extend staged lease: %v", err)
	}
	if err := fixture.queue.stageAutomaticDiscoveryAcknowledgment(
		t.Context(),
		leaseID,
		"worker",
		"session",
		true,
	); err != nil {
		t.Fatalf("retry identical acknowledgment: %v", err)
	}
	requireAutomaticDiscoverySettlementIntentUnchanged(t, fixture.engine, leaseID, staged)
	removeAutomaticDiscoverySettlementLease(t, fixture.queue, leaseID)
	if err := fixture.queue.stageAutomaticDiscoveryAcknowledgment(
		t.Context(),
		leaseID,
		"worker",
		"session",
		true,
	); err != nil {
		t.Fatalf("retry retained acknowledgment: %v", err)
	}
	if err := fixture.queue.stageAutomaticDiscoveryTerminalSettlement(
		t.Context(),
		leaseID,
		automaticDiscoveryTerminalRequest(data),
	); !errors.Is(err, errLeaseDispositionConflict) {
		t.Fatalf("terminal replacement conflict error = %v", err)
	}
	requireAutomaticDiscoverySettlementIntentUnchanged(t, fixture.engine, leaseID, staged)
}
