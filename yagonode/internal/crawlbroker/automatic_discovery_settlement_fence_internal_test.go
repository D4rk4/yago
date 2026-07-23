package crawlbroker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type automaticDiscoverySettlementBlockingEngine struct {
	*scriptedEngine
	mutex sync.Mutex
	block bool
}

func (engine *automaticDiscoverySettlementBlockingEngine) blockNextUpdate() {
	engine.mutex.Lock()
	engine.block = true
	engine.mutex.Unlock()
}

func (engine *automaticDiscoverySettlementBlockingEngine) Update(
	ctx context.Context,
	mutate func(vault.EngineTxn) error,
) error {
	engine.mutex.Lock()
	block := engine.block
	engine.block = false
	engine.mutex.Unlock()
	if block {
		<-ctx.Done()

		return fmt.Errorf("block settlement update: %w", ctx.Err())
	}

	return engine.scriptedEngine.Update(ctx, mutate)
}

func TestAutomaticDiscoverySettlementIntentFencesNegativeAcknowledgment(
	t *testing.T,
) {
	for _, terminal := range []bool{false, true} {
		name := "acknowledgment"
		if terminal {
			name = "terminal"
		}
		t.Run(name, func(t *testing.T) {
			fixture := scriptedQueue(t)
			target := fmt.Sprintf("https://nak-fence-%t.example/", terminal)
			data, leaseID := automaticDiscoverySettlementLease(t, fixture.queue, target)
			if terminal {
				if err := fixture.queue.stageAutomaticDiscoveryTerminalSettlement(
					t.Context(),
					leaseID,
					automaticDiscoveryTerminalRequest(data),
				); err != nil {
					t.Fatalf("stage terminal settlement: %v", err)
				}
			} else if err := fixture.queue.stageAutomaticDiscoveryAcknowledgment(
				t.Context(),
				leaseID,
				"worker",
				"session",
				true,
			); err != nil {
				t.Fatalf("stage acknowledgment: %v", err)
			}
			if err := fixture.queue.deferLeaseForOwner(
				t.Context(),
				leaseID,
				"worker",
				"session",
			); !errors.Is(err, errLeaseDispositionConflict) {
				t.Fatalf("competing negative acknowledgment error = %v", err)
			}
			requireAutomaticDiscoverySettlementComplete(
				t,
				fixture.queue,
				target,
				leaseID,
				terminal,
			)
			if fixture.queue.workerLeases.reached("worker", "session", 1) {
				t.Fatal("settled lease remained in the worker catalog")
			}
		})
	}
}

func TestAutomaticDiscoverySettlementIntentFencesLeaseSweep(t *testing.T) {
	fixture := scriptedQueue(t)
	target := "https://sweep-fence.example/"
	requireAutomaticDiscoveryAdmission(t, fixture.queue, target, false)
	_, leaseID, found, err := fixture.queue.leasePopForSession(
		t.Context(),
		"worker",
		"",
	)
	if err != nil || !found {
		t.Fatalf("lease automatic discovery = %t, %v", found, err)
	}
	if err := fixture.queue.stageAutomaticDiscoveryAcknowledgment(
		t.Context(),
		leaseID,
		"",
		"",
		false,
	); err != nil {
		t.Fatalf("stage acknowledgment: %v", err)
	}
	if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		record, found, err := fixture.queue.leases.Get(tx, vault.Key(leaseID))
		if err != nil || !found {
			return fmt.Errorf("read staged lease: %w", err)
		}
		record.ExpiresAtUnixNano = nowFunc().Add(-time.Second).UnixNano()

		return fixture.queue.leases.Put(tx, vault.Key(leaseID), record)
	}); err != nil {
		t.Fatalf("expire staged lease: %v", err)
	}
	if err := fixture.queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("sweep staged settlement: %v", err)
	}
	requireAutomaticDiscoverySettlementComplete(t, fixture.queue, target, leaseID, false)
	_, _, found, err = fixture.queue.leasePopForSession(t.Context(), "other", "session")
	if err != nil || found {
		t.Fatalf("settled order was requeued = %t, %v", found, err)
	}
}

func TestAutomaticDiscoverySettlementIntentFencesSessionAdoption(t *testing.T) {
	fixture := scriptedQueue(t)
	target := "https://adoption-fence.example/"
	_, leaseID := automaticDiscoverySettlementLease(t, fixture.queue, target)
	if err := fixture.queue.stageAutomaticDiscoveryAcknowledgment(
		t.Context(),
		leaseID,
		"worker",
		"session",
		true,
	); err != nil {
		t.Fatalf("stage acknowledgment: %v", err)
	}
	adopted, err := fixture.queue.adoptWorkerSession(
		t.Context(),
		"worker",
		"replacement-session",
	)
	if err != nil {
		t.Fatalf("adopt worker session: %v", err)
	}
	if len(adopted) != 0 {
		t.Fatalf("adopted settled leases = %d", len(adopted))
	}
	requireAutomaticDiscoverySettlementComplete(t, fixture.queue, target, leaseID, false)
	if fixture.queue.workerLeases.reached("worker", "session", 1) ||
		fixture.queue.workerLeases.reached("worker", "replacement-session", 1) {
		t.Fatal("settled lease remained in the worker catalog")
	}
}

func TestAutomaticDiscoverySettlementFenceSurfacesNegativeAcknowledgmentIntentRead(
	t *testing.T,
) {
	fixture := scriptedQueue(t)
	leaseID := leaseOneForSession(
		t,
		fixture.queue,
		"nak-intent-read",
		"worker",
		"session",
	)
	fixture.engine.readErrors[discoverySettlementBucket] = errors.New("read failed")
	if err := fixture.queue.deferLeaseForOwner(
		t.Context(),
		leaseID,
		"worker",
		"session",
	); err == nil {
		t.Fatal("negative acknowledgment intent read failure was hidden")
	}
}

func TestAutomaticDiscoverySettlementFenceRecoversNegativeAcknowledgmentFailure(
	t *testing.T,
) {
	fixture := scriptedQueue(t)
	target := "https://nak-recovery-failure.example/"
	_, leaseID := automaticDiscoverySettlementLease(t, fixture.queue, target)
	stageAutomaticDiscoveryAcknowledgment(t, fixture.queue, leaseID, "session")
	fixture.engine.deleteErrors[activeDiscoveryKeyBucket] = errors.New("delete failed")
	if err := fixture.queue.deferLeaseForOwner(
		t.Context(),
		leaseID,
		"worker",
		"session",
	); err == nil {
		t.Fatal("negative acknowledgment recovery failure was hidden")
	}
	delete(fixture.engine.deleteErrors, activeDiscoveryKeyBucket)
	if err := fixture.queue.resolveAutomaticDiscoverySettlement(
		t.Context(),
		leaseID,
	); err != nil {
		t.Fatalf("recover negative acknowledgment fence: %v", err)
	}
	requireAutomaticDiscoverySettlementComplete(
		t,
		fixture.queue,
		target,
		leaseID,
		false,
	)
}

func TestAutomaticDiscoverySettlementFenceRecoversSessionAdoptionFailure(
	t *testing.T,
) {
	fixture := scriptedQueue(t)
	target := "https://adoption-recovery-failure.example/"
	_, leaseID := automaticDiscoverySettlementLease(t, fixture.queue, target)
	stageAutomaticDiscoveryAcknowledgment(t, fixture.queue, leaseID, "session")
	fixture.engine.deleteErrors[activeDiscoveryKeyBucket] = errors.New("delete failed")
	if _, err := fixture.queue.adoptWorkerSession(
		t.Context(),
		"worker",
		"replacement-session",
	); err == nil {
		t.Fatal("session adoption recovery failure was hidden")
	}
	delete(fixture.engine.deleteErrors, activeDiscoveryKeyBucket)
	adopted, err := fixture.queue.adoptWorkerSession(
		t.Context(),
		"worker",
		"replacement-session",
	)
	if err != nil || len(adopted) != 0 {
		t.Fatalf("recover session adoption = %d, %v", len(adopted), err)
	}
	requireAutomaticDiscoverySettlementComplete(
		t,
		fixture.queue,
		target,
		leaseID,
		false,
	)
}

func TestAutomaticDiscoverySettlementFenceSurfacesLeaseSweepIntentRead(
	t *testing.T,
) {
	fixture := scriptedQueue(t)
	leaseID := leaseOne(t, fixture.queue, "sweep-intent-read", "worker")
	fixture.engine.readErrors[discoverySettlementBucket] = errors.New("read failed")
	if _, err := fixture.queue.requeueLeaseChunk(
		t.Context(),
		[]vault.Key{vault.Key(leaseID)},
		func(leaseRecord) bool { return true },
	); err == nil {
		t.Fatal("lease sweep intent read failure was hidden")
	}
}

func TestAutomaticDiscoverySettlementFenceRecoversLeaseSweepFailure(t *testing.T) {
	fixture := scriptedQueue(t)
	target := "https://sweep-recovery-failure.example/"
	requireAutomaticDiscoveryAdmission(t, fixture.queue, target, false)
	_, leaseID, found, err := fixture.queue.leasePopForSession(
		t.Context(),
		"worker",
		"",
	)
	if err != nil || !found {
		t.Fatalf("lease automatic discovery = %t, %v", found, err)
	}
	stageAutomaticDiscoveryAcknowledgment(t, fixture.queue, leaseID, "")
	fixture.engine.deleteErrors[activeDiscoveryKeyBucket] = errors.New("delete failed")
	if _, err := fixture.queue.requeueLeaseChunk(
		t.Context(),
		[]vault.Key{vault.Key(leaseID)},
		func(leaseRecord) bool { return true },
	); err == nil {
		t.Fatal("lease sweep recovery failure was hidden")
	}
	delete(fixture.engine.deleteErrors, activeDiscoveryKeyBucket)
	if err := fixture.queue.resolveAutomaticDiscoverySettlement(
		t.Context(),
		leaseID,
	); err != nil {
		t.Fatalf("recover lease sweep fence: %v", err)
	}
	requireAutomaticDiscoverySettlementComplete(
		t,
		fixture.queue,
		target,
		leaseID,
		false,
	)
}

func stageAutomaticDiscoveryAcknowledgment(
	t *testing.T,
	queue *DurableOrderQueue,
	leaseID string,
	workerSessionID string,
) {
	t.Helper()
	if err := queue.stageAutomaticDiscoveryAcknowledgment(
		t.Context(),
		leaseID,
		"worker",
		workerSessionID,
		workerSessionID != "",
	); err != nil {
		t.Fatalf("stage acknowledgment: %v", err)
	}
}

func TestAutomaticDiscoverySettlementRecoveryAfterCallerCancellationIsBounded(
	t *testing.T,
) {
	engine := &automaticDiscoverySettlementBlockingEngine{
		scriptedEngine: newScriptedEngine(),
	}
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("open blocking storage: %v", err)
	}
	queue, err := newDurableOrderQueue(storage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("open blocking queue: %v", err)
	}
	target := "https://cancelled-settlement.example/"
	_, leaseID := automaticDiscoverySettlementLease(t, queue, target)
	ctx, cancel := context.WithCancel(t.Context())
	previousHook := afterAutomaticDiscoverySettlementStage
	afterAutomaticDiscoverySettlementStage = func() {
		cancel()
		engine.blockNextUpdate()
	}
	t.Cleanup(func() {
		afterAutomaticDiscoverySettlementStage = previousHook
		cancel()
	})
	started := time.Now()
	_, err = queue.ackLeaseWithOwner(ctx, leaseID, "worker", "session")
	elapsed := time.Since(started)
	afterAutomaticDiscoverySettlementStage = previousHook
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("bounded settlement error = %v", err)
	}
	if elapsed > automaticDiscoverySettlementRecoveryBudget+time.Second {
		t.Fatalf("bounded settlement elapsed = %s", elapsed)
	}
	requireAutomaticDiscoverySettlementState(
		t,
		queue,
		automaticDiscoverySettlementStateExpectation{
			target:   target,
			leaseID:  leaseID,
			active:   true,
			settling: true,
		},
	)
	reopenedStorage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("reopen blocking storage: %v", err)
	}
	reopened, err := newDurableOrderQueue(reopenedStorage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("reopen blocking queue: %v", err)
	}
	requireAutomaticDiscoverySettlementComplete(t, reopened, target, leaseID, false)
	if reopened.workerLeases.reached("worker", "session", 1) {
		t.Fatal("reopened worker catalog retained the settled lease")
	}
}
