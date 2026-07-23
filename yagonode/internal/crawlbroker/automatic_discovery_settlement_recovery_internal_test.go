package crawlbroker

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type automaticDiscoverySettlementFailure struct {
	name   string
	bucket vault.Name
	remove bool
	key    vault.Key
}

func (failure automaticDiscoverySettlementFailure) apply(engine *scriptedEngine) {
	if failure.remove {
		engine.deleteErrors[failure.bucket] = errors.New("mutation failed")

		return
	}
	if failure.key != nil {
		engine.putKeyErrors[failure.bucket] = map[string]error{
			string(failure.key): errors.New("mutation failed"),
		}

		return
	}
	engine.putErrors[failure.bucket] = errors.New("mutation failed")
}

func (failure automaticDiscoverySettlementFailure) clear(engine *scriptedEngine) {
	delete(engine.deleteErrors, failure.bucket)
	delete(engine.putErrors, failure.bucket)
	delete(engine.putKeyErrors, failure.bucket)
}

func automaticDiscoverySettlementFailures(
	terminal bool,
) []automaticDiscoverySettlementFailure {
	failures := []automaticDiscoverySettlementFailure{
		{name: "control target", bucket: leaseControlTargetBucket},
		{name: "lease deletion", bucket: leaseBucket, remove: true},
		{name: "leased ownership", bucket: leasedDiscoveryKeyBucket, remove: true},
		{name: "settlement record", bucket: leaseSettlementBucket},
		{name: "settlement order", bucket: leaseSettlementOrderBucket},
		{
			name:   "settlement sequence",
			bucket: seqBucket,
			key:    leaseSettlementNextKey,
		},
		{name: "active ownership", bucket: activeDiscoveryKeyBucket, remove: true},
		{name: "intent release", bucket: discoverySettlementBucket, remove: true},
	}
	if terminal {
		return failures
	}
	withExpiry := make([]automaticDiscoverySettlementFailure, 0, len(failures)+1)
	withExpiry = append(withExpiry, failures[:5]...)
	withExpiry = append(
		withExpiry,
		automaticDiscoverySettlementFailure{
			name:   "settlement expiry",
			bucket: leaseSettlementExpiryBucket,
		},
	)

	return append(withExpiry, failures[5:]...)
}

func settleAutomaticDiscoveryLease(
	ctx testing.TB,
	queue *DurableOrderQueue,
	terminal bool,
	data []byte,
	leaseID string,
) error {
	if terminal {
		_, err := queue.prepareTerminalLeaseSettlement(
			ctx.Context(),
			leaseID,
			automaticDiscoveryTerminalRequest(data),
		)

		return err
	}
	_, err := queue.ackLeaseWithOwner(
		ctx.Context(),
		leaseID,
		"worker",
		"session",
	)

	return err
}

func requireAutomaticDiscoverySettlementComplete(
	t *testing.T,
	queue *DurableOrderQueue,
	target string,
	leaseID string,
	terminal bool,
) {
	t.Helper()
	requireAutomaticDiscoverySettlementState(
		t,
		queue,
		automaticDiscoverySettlementStateExpectation{
			target:  target,
			leaseID: leaseID,
		},
	)
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, leased, err := queue.leases.Get(tx, vault.Key(leaseID))
		if err != nil {
			return fmt.Errorf("read automatic discovery lease: %w", err)
		}
		settlement, settled, err := queue.leaseSettlements.Get(tx, vault.Key(leaseID))
		if err != nil {
			return fmt.Errorf("read automatic discovery lease settlement: %w", err)
		}
		if leased || !settled || settlement.Outcome != leaseSettlementAcknowledged ||
			settlement.Terminal != terminal {
			return fmt.Errorf(
				"lease settlement = leased %t, settled %t, outcome %d, terminal %t",
				leased,
				settled,
				settlement.Outcome,
				settlement.Terminal,
			)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAutomaticDiscoverySettlementPartialMutationsConverge(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		disposition := "acknowledgment"
		if terminal {
			disposition = "terminal"
		}
		for _, failure := range automaticDiscoverySettlementFailures(terminal) {
			for _, recovery := range []string{"retry", "reopen"} {
				t.Run(disposition+"/"+failure.name+"/"+recovery, func(t *testing.T) {
					runAutomaticDiscoverySettlementPartialMutation(
						t,
						terminal,
						failure,
						recovery,
					)
				})
			}
		}
	}
}

func runAutomaticDiscoverySettlementPartialMutation(
	t *testing.T,
	terminal bool,
	failure automaticDiscoverySettlementFailure,
	recovery string,
) {
	t.Helper()
	fixture := scriptedQueue(t)
	target := fmt.Sprintf(
		"https://partial-settlement-%t-%s.example/",
		terminal,
		recovery,
	)
	data, leaseID := automaticDiscoverySettlementLease(t, fixture.queue, target)
	failure.apply(fixture.engine)
	if err := settleAutomaticDiscoveryLease(
		t,
		fixture.queue,
		terminal,
		data,
		leaseID,
	); err == nil {
		t.Fatal("partial settlement failure was hidden")
	}
	requireAutomaticDiscoverySettlementState(
		t,
		fixture.queue,
		automaticDiscoverySettlementStateExpectation{
			target:   target,
			leaseID:  leaseID,
			active:   failure.bucket != discoverySettlementBucket,
			settling: true,
		},
	)
	failure.clear(fixture.engine)
	recovered := fixture.queue
	if recovery == "retry" {
		if err := settleAutomaticDiscoveryLease(
			t,
			recovered,
			terminal,
			data,
			leaseID,
		); err != nil {
			t.Fatalf("retry partial settlement: %v", err)
		}
	} else {
		recovered = reopenAutomaticDiscoveryQueue(t, fixture.engine)
	}
	requireAutomaticDiscoverySettlementComplete(
		t,
		recovered,
		target,
		leaseID,
		terminal,
	)
}

func TestAutomaticDiscoverySettlementCrashAfterStageConverges(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		name := "acknowledgment"
		if terminal {
			name = "terminal"
		}
		t.Run(name, func(t *testing.T) {
			fixture := scriptedQueue(t)
			target := fmt.Sprintf("https://crash-after-stage-%t.example/", terminal)
			data, leaseID := automaticDiscoverySettlementLease(t, fixture.queue, target)
			var err error
			if terminal {
				err = fixture.queue.stageAutomaticDiscoveryTerminalSettlement(
					t.Context(),
					leaseID,
					automaticDiscoveryTerminalRequest(data),
				)
			} else {
				err = fixture.queue.stageAutomaticDiscoveryAcknowledgment(
					t.Context(),
					leaseID,
					"worker",
					"session",
					true,
				)
			}
			if err != nil {
				t.Fatalf("stage automatic discovery settlement: %v", err)
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
			reopened := reopenAutomaticDiscoveryQueue(t, fixture.engine)
			requireAutomaticDiscoverySettlementComplete(
				t,
				reopened,
				target,
				leaseID,
				terminal,
			)
		})
	}
}

func TestAutomaticDiscoverySettlementHistoryBeforeLeaseDeletionConverges(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		name := "acknowledgment"
		if terminal {
			name = "terminal"
		}
		t.Run(name, func(t *testing.T) {
			runAutomaticDiscoverySettlementHistoryBeforeLeaseDeletion(t, terminal)
		})
	}
}

func runAutomaticDiscoverySettlementHistoryBeforeLeaseDeletion(
	t *testing.T,
	terminal bool,
) {
	t.Helper()
	fixture := scriptedQueue(t)
	target := fmt.Sprintf("https://history-before-lease-%t.example/", terminal)
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
	if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		intent, found, err := fixture.queue.discoverySettlements.Get(
			tx,
			vault.Key(leaseID),
		)
		if err != nil {
			return fmt.Errorf("read staged automatic discovery settlement: %w", err)
		}
		if !found {
			return fmt.Errorf("staged settlement intent is missing")
		}
		settlement := intent.Settlement
		settlement.Sequence = 7
		settlement.SettledAtUnixNano = nowFunc().UnixNano()
		if err := fixture.queue.leaseSettlements.Put(
			tx,
			vault.Key(leaseID),
			settlement,
		); err != nil {
			return fmt.Errorf("seed automatic discovery lease settlement: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("seed partial settlement history: %v", err)
	}
	reopened := reopenAutomaticDiscoveryQueue(t, fixture.engine)
	requireAutomaticDiscoverySettlementComplete(
		t,
		reopened,
		target,
		leaseID,
		terminal,
	)
	requireAutomaticDiscoverySettlementIndexes(
		t,
		reopened,
		leaseID,
		7,
		terminal,
	)
}

func requireAutomaticDiscoverySettlementIndexes(
	t *testing.T,
	queue *DurableOrderQueue,
	leaseID string,
	sequence uint64,
	terminal bool,
) {
	t.Helper()
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		indexedLeaseID, indexed, err := queue.leaseSettlementOrder.Get(
			tx,
			orderKey(sequence),
		)
		if err != nil {
			return fmt.Errorf("read automatic discovery settlement order: %w", err)
		}
		next, _, err := queue.seq.Get(tx, leaseSettlementNextKey)
		if err != nil {
			return fmt.Errorf("read automatic discovery settlement sequence: %w", err)
		}
		migrationNext, _, err := queue.seq.Get(tx, leaseSettlementMigrationNextKey)
		if err != nil {
			return fmt.Errorf("read automatic discovery settlement migration: %w", err)
		}
		if !indexed || string(indexedLeaseID) != leaseID ||
			next != sequence+1 || migrationNext != sequence+1 {
			return fmt.Errorf(
				"settlement indexes = %q, %t, next %d, migration %d",
				indexedLeaseID,
				indexed,
				next,
				migrationNext,
			)
		}
		if terminal {
			return nil
		}
		settlement, _, err := queue.leaseSettlements.Get(tx, vault.Key(leaseID))
		if err != nil {
			return fmt.Errorf("read automatic discovery lease settlement: %w", err)
		}
		_, expiring, err := queue.leaseSettlementExpiry.Get(
			tx,
			leaseSettlementExpiryKey(settlement),
		)
		if err != nil {
			return fmt.Errorf("read automatic discovery settlement expiry: %w", err)
		}
		if !expiring {
			return fmt.Errorf("settlement expiry index is missing")
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

type automaticDiscoverySettlementRollbackEngine struct {
	*scriptedEngine
	armed          bool
	update         int
	rollbackUpdate map[int]bool
}

func (engine *automaticDiscoverySettlementRollbackEngine) Update(
	ctx context.Context,
	mutate func(vault.EngineTxn) error,
) error {
	if !engine.armed {
		return engine.scriptedEngine.Update(ctx, mutate)
	}
	engine.update++
	if !engine.rollbackUpdate[engine.update] {
		return engine.scriptedEngine.Update(ctx, mutate)
	}
	before := cloneScriptedBuckets(engine.buckets)
	err := mutate(scriptedTxn{engine: engine.scriptedEngine, writable: true})
	engine.buckets = before
	if err != nil {
		return err
	}

	return errors.New("commit failed")
}

func TestAutomaticDiscoverySettlementCommitFailureKeepsWorkerLeaseAccounting(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		name := "acknowledgment"
		if terminal {
			name = "terminal"
		}
		t.Run(name, func(t *testing.T) {
			runAutomaticDiscoverySettlementCommitFailure(t, terminal)
		})
	}
}

func runAutomaticDiscoverySettlementCommitFailure(t *testing.T, terminal bool) {
	t.Helper()
	engine := &automaticDiscoverySettlementRollbackEngine{
		scriptedEngine: newScriptedEngine(),
		rollbackUpdate: map[int]bool{2: true, 3: true},
	}
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("open rollback storage: %v", err)
	}
	queue, err := newDurableOrderQueue(storage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("open rollback queue: %v", err)
	}
	target := fmt.Sprintf("https://commit-failure-%t.example/", terminal)
	data, leaseID := automaticDiscoverySettlementLease(t, queue, target)
	if !queue.workerLeases.reached("worker", "session", 1) {
		t.Fatal("leased worker was not counted")
	}
	engine.armed = true
	if err := settleAutomaticDiscoveryLease(
		t,
		queue,
		terminal,
		data,
		leaseID,
	); err == nil {
		t.Fatal("completion commit failure was hidden")
	}
	if !queue.workerLeases.reached("worker", "session", 1) {
		t.Fatal("failed completion released in-memory worker accounting")
	}
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, leased, err := queue.leases.Get(tx, vault.Key(leaseID))
		if err != nil {
			return fmt.Errorf("read automatic discovery lease: %w", err)
		}
		_, staged, err := queue.discoverySettlements.Get(tx, vault.Key(leaseID))
		if err != nil {
			return fmt.Errorf("read staged automatic discovery settlement: %w", err)
		}
		if !leased || !staged {
			return fmt.Errorf(
				"rollback state = leased %t, staged %t",
				leased,
				staged,
			)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	engine.armed = false
	if err := settleAutomaticDiscoveryLease(
		t,
		queue,
		terminal,
		data,
		leaseID,
	); err != nil {
		t.Fatalf("retry completion after commit recovery: %v", err)
	}
	if queue.workerLeases.reached("worker", "session", 1) {
		t.Fatal("committed completion retained in-memory worker accounting")
	}
	requireAutomaticDiscoverySettlementComplete(
		t,
		queue,
		target,
		leaseID,
		terminal,
	)
}
