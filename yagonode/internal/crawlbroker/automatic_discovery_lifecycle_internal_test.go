package crawlbroker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func automaticDiscoveryOrder(target string) yagocrawlcontract.CrawlOrder {
	order := testOrder("automatic")
	order.Priority = yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery
	order.Requests[0].URL = target

	return order
}

func requireAutomaticDiscoveryAdmission(
	t *testing.T,
	queue *DurableOrderQueue,
	target string,
	duplicate bool,
) {
	t.Helper()
	got, err := queue.PublishOnce(t.Context(), target, automaticDiscoveryOrder(target))
	if err != nil || got != duplicate {
		t.Fatalf("publish automatic discovery = %t, %v, want %t, nil", got, err, duplicate)
	}
}

func requireAutomaticDiscoveryLeaseIndex(
	t *testing.T,
	queue *DurableOrderQueue,
	target string,
	leaseID string,
	found bool,
) {
	t.Helper()
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		indexedLeaseID, indexed, err := queue.leasedDiscoveryKeys.Get(
			tx,
			vault.Key(target),
		)
		if err != nil {
			return fmt.Errorf("read leased discovery index: %w", err)
		}
		if indexed != found || indexed && string(indexedLeaseID) != leaseID {
			t.Fatalf(
				"leased discovery index = %q, %t, want %q, %t",
				indexedLeaseID,
				indexed,
				leaseID,
				found,
			)
		}

		return nil
	}); err != nil {
		t.Fatalf("read leased discovery index: %v", err)
	}
}

func TestAutomaticDiscoveryCoalescesOnlyWhilePendingOrLeased(t *testing.T) {
	queue := memQueue(t)
	target := "https://discovery.example/page"
	requireAutomaticDiscoveryAdmission(t, queue, target, false)
	requireAutomaticDiscoveryAdmission(t, queue, target, true)

	_, leaseID, found, err := queue.leasePopForSession(t.Context(), "worker", "session")
	if err != nil || !found {
		t.Fatalf("lease automatic discovery = %q, %t, %v", leaseID, found, err)
	}
	requireAutomaticDiscoveryLeaseIndex(t, queue, target, leaseID, true)
	requireAutomaticDiscoveryAdmission(t, queue, target, true)
	if _, err := queue.ackLeaseWithOwner(
		t.Context(),
		leaseID,
		"worker",
		"session",
	); err != nil {
		t.Fatalf("ack automatic discovery: %v", err)
	}
	requireAutomaticDiscoveryLeaseIndex(t, queue, target, "", false)
	requireAutomaticDiscoveryAdmission(t, queue, target, false)
}

func TestAutomaticDiscoveryRequeueRetainsCoalescing(t *testing.T) {
	queue := memQueue(t)
	target := "https://retry.example/page"
	requireAutomaticDiscoveryAdmission(t, queue, target, false)
	_, _, found, err := queue.leasePopForSession(t.Context(), "worker", "session")
	if err != nil || !found {
		t.Fatalf("lease automatic discovery = %t, %v", found, err)
	}
	if err := queue.requeueLeasesMatching(
		t.Context(),
		func(leaseRecord) bool { return true },
	); err != nil {
		t.Fatalf("requeue automatic discovery: %v", err)
	}
	requireAutomaticDiscoveryAdmission(t, queue, target, true)
}

func TestAutomaticDiscoveryTerminalFailureAndCancellationPermitRetry(t *testing.T) {
	cases := []struct {
		name  string
		state yagocrawlcontract.CrawlRunState
		tally yagocrawlcontract.CrawlRunTally
	}{
		{
			name:  "failed",
			state: yagocrawlcontract.CrawlRunFinished,
			tally: yagocrawlcontract.CrawlRunTally{Failed: 1},
		},
		{
			name:  "cancelled",
			state: yagocrawlcontract.CrawlRunCancelled,
			tally: yagocrawlcontract.CrawlRunTally{},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			queue := memQueue(t)
			target := "https://" + test.name + ".example/page"
			requireAutomaticDiscoveryAdmission(t, queue, target, false)
			data, leaseID, found, err := queue.leasePopForSession(
				t.Context(),
				"worker",
				"session",
			)
			if err != nil || !found {
				t.Fatalf("lease automatic discovery = %t, %v", found, err)
			}
			identity := sha256.Sum256(data)
			request := terminalLeaseRequest{
				Outcome:         leaseSettlementAcknowledged,
				OrderIdentity:   identity[:],
				WorkerID:        "worker",
				WorkerSessionID: "session",
				State:           test.state,
				Tally:           test.tally,
			}
			if _, err := queue.prepareTerminalLeaseSettlement(
				t.Context(),
				leaseID,
				request,
			); err != nil {
				t.Fatalf("settle automatic discovery: %v", err)
			}
			requireAutomaticDiscoveryAdmission(t, queue, target, false)
		})
	}
}

func TestAutomaticDiscoveryCoalescingSurvivesQueueRestart(t *testing.T) {
	engine := newScriptedEngine()
	firstVault, err := vault.New(engine)
	if err != nil {
		t.Fatalf("first vault: %v", err)
	}
	first, err := newDurableOrderQueue(firstVault, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("first queue: %v", err)
	}
	target := "https://restart.example/page"
	requireAutomaticDiscoveryAdmission(t, first, target, false)

	secondVault, err := vault.New(engine)
	if err != nil {
		t.Fatalf("second vault: %v", err)
	}
	second, err := newDurableOrderQueue(secondVault, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("second queue: %v", err)
	}
	requireAutomaticDiscoveryAdmission(t, second, target, true)
	_, leaseID, found, err := second.leasePopForSession(t.Context(), "worker", "session")
	if err != nil || !found {
		t.Fatalf("restart lease = %t, %v", found, err)
	}

	thirdVault, err := vault.New(engine)
	if err != nil {
		t.Fatalf("third vault: %v", err)
	}
	third, err := newDurableOrderQueue(thirdVault, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("third queue: %v", err)
	}
	requireAutomaticDiscoveryAdmission(t, third, target, true)
	if _, err := third.ackLeaseWithOwner(
		t.Context(),
		leaseID,
		"worker",
		"session",
	); err != nil {
		t.Fatalf("restart ack: %v", err)
	}
	requireAutomaticDiscoveryAdmission(t, third, target, false)
}

func TestOrphanAutomaticDiscoveryKeyRepairsAfterQueueRestart(t *testing.T) {
	engine := newScriptedEngine()
	firstVault, err := vault.New(engine)
	if err != nil {
		t.Fatalf("first vault: %v", err)
	}
	first, err := newDurableOrderQueue(firstVault, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("first queue: %v", err)
	}
	target := "https://orphan.example/page"
	if err := first.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return first.activeDiscoveryKeys.Put(tx, vault.Key(target), 77)
	}); err != nil {
		t.Fatalf("seed orphan discovery key: %v", err)
	}

	secondVault, err := vault.New(engine)
	if err != nil {
		t.Fatalf("second vault: %v", err)
	}
	second, err := newDurableOrderQueue(secondVault, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("second queue: %v", err)
	}
	requireAutomaticDiscoveryAdmission(t, second, target, false)
	requireAutomaticDiscoveryAdmission(t, second, target, true)
}

func TestMissingPendingDiscoverySidecarRepairsBeforeLease(t *testing.T) {
	queue := memQueue(t)
	target := "https://sidecar.example/page"
	requireAutomaticDiscoveryAdmission(t, queue, target, false)
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		_, err := queue.pendingDiscoveryKeys.Delete(tx, orderKey(0))
		if err != nil {
			return fmt.Errorf("delete pending discovery sidecar: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("remove pending discovery sidecar: %v", err)
	}
	requireAutomaticDiscoveryAdmission(t, queue, target, true)
	_, leaseID, found, err := queue.leasePopForSession(t.Context(), "worker", "session")
	if err != nil || !found {
		t.Fatalf("lease repaired discovery = %t, %v", found, err)
	}
	record, found := leaseRecordFor(t, queue, leaseID)
	if !found || record.DiscoveryKey != target {
		t.Fatalf("repaired discovery lease = %+v, %t", record, found)
	}
	if _, err := queue.ackLeaseWithOwner(
		t.Context(),
		leaseID,
		"worker",
		"session",
	); err != nil {
		t.Fatalf("ack repaired discovery: %v", err)
	}
	requireAutomaticDiscoveryAdmission(t, queue, target, false)
}

func seedAutomaticDiscoveryActiveWithoutOrder(
	t *testing.T,
	queue *DurableOrderQueue,
	target string,
) {
	t.Helper()
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return queue.activeDiscoveryKeys.Put(tx, vault.Key(target), 77)
	}); err != nil {
		t.Fatalf("seed active key: %v", err)
	}
}

func seedAutomaticDiscoveryOrderWithoutActive(
	t *testing.T,
	queue *DurableOrderQueue,
	target string,
) {
	t.Helper()
	order := automaticDiscoveryOrder(target)
	data, err := yagocrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal order: %v", err)
	}
	if err := queue.persistAutomaticDiscoveryIntent(t.Context(), target, data); err != nil {
		t.Fatalf("persist admission intent: %v", err)
	}
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		_, err := queue.enqueueTx(tx, data, order.Priority)

		return err
	}); err != nil {
		t.Fatalf("seed pending order: %v", err)
	}
}

func seedAutomaticDiscoveryOwnershipWithoutLease(
	t *testing.T,
	queue *DurableOrderQueue,
	target string,
) {
	t.Helper()
	requireAutomaticDiscoveryAdmission(t, queue, target, false)
	_, leaseID, found, err := queue.leasePopForSession(
		t.Context(),
		"worker",
		"session",
	)
	if err != nil || !found {
		t.Fatalf("lease discovery = %t, %v", found, err)
	}
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		_, err := queue.leases.Delete(tx, vault.Key(leaseID))
		if err != nil {
			return fmt.Errorf("delete automatic discovery lease: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("delete lease: %v", err)
	}
}

func seedAutomaticDiscoveryLeaseWithoutOwnership(
	t *testing.T,
	queue *DurableOrderQueue,
	target string,
) {
	t.Helper()
	requireAutomaticDiscoveryAdmission(t, queue, target, false)
	if _, _, found, err := queue.leasePopForSession(
		t.Context(),
		"worker",
		"session",
	); err != nil || !found {
		t.Fatalf("lease discovery = %t, %v", found, err)
	}
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		_, err := queue.activeDiscoveryKeys.Delete(tx, vault.Key(target))
		if err != nil {
			return fmt.Errorf("release automatic discovery ownership: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("release active key: %v", err)
	}
}

func requireAutomaticDiscoveryPartialStateConvergence(
	t *testing.T,
	name string,
	duplicate bool,
	seed func(*testing.T, *DurableOrderQueue, string),
) {
	t.Helper()
	engine := newScriptedEngine()
	queue := reopenAutomaticDiscoveryQueue(t, engine)
	target := "https://partial-" + name + ".example/page"
	seed(t, queue, target)
	recovered := reopenAutomaticDiscoveryQueue(t, engine)
	requireAutomaticDiscoveryAdmission(t, recovered, target, duplicate)
	requireAutomaticDiscoveryAdmission(t, recovered, target, true)
}

func reopenAutomaticDiscoveryQueue(
	t *testing.T,
	engine *scriptedEngine,
) *DurableOrderQueue {
	t.Helper()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("reopen persistent queue storage: %v", err)
	}
	queue, err := newDurableOrderQueue(storage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("reopen persistent queue: %v", err)
	}

	return queue
}

func TestAutomaticDiscoveryPartialShardStatesConvergeAfterReopen(t *testing.T) {
	tests := []struct {
		name      string
		duplicate bool
		seed      func(*testing.T, *DurableOrderQueue, string)
	}{
		{name: "active-without-order", seed: seedAutomaticDiscoveryActiveWithoutOrder},
		{
			name:      "order-without-active",
			duplicate: true,
			seed:      seedAutomaticDiscoveryOrderWithoutActive,
		},
		{
			name: "lease-deleted-before-key-release",
			seed: seedAutomaticDiscoveryOwnershipWithoutLease,
		},
		{
			name:      "key-released-before-lease-delete",
			duplicate: true,
			seed:      seedAutomaticDiscoveryLeaseWithoutOwnership,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requireAutomaticDiscoveryPartialStateConvergence(
				t,
				test.name,
				test.duplicate,
				test.seed,
			)
		})
	}
}

func TestUnidentifiableLegacyKeyRemainsInPermanentNamespace(t *testing.T) {
	queue := memQueue(t)
	target := "https://stale-legacy.example/page"
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return queue.keys.Put(tx, vault.Key(target), 91)
	}); err != nil {
		t.Fatalf("seed stale legacy key: %v", err)
	}
	requireAutomaticDiscoveryAdmission(t, queue, target, false)
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, found, err := queue.keys.Get(tx, vault.Key(target))
		if err != nil {
			return fmt.Errorf("read legacy idempotency key: %w", err)
		}
		if !found {
			t.Fatal("unidentifiable legacy key was removed")
		}

		return nil
	}); err != nil {
		t.Fatalf("read legacy key: %v", err)
	}
}

func TestAutomaticDiscoveryDoesNotEraseManualIdempotencyKey(t *testing.T) {
	queue := memQueue(t)
	target := "https://idempotency-collision.example/page"
	manual := testOrder("manual-idempotency")
	if duplicate, err := queue.PublishOnce(t.Context(), target, manual); err != nil || duplicate {
		t.Fatalf("publish manual order = %t, %v", duplicate, err)
	}
	_, leaseID, found, err := queue.leasePopForSession(t.Context(), "worker", "session")
	if err != nil || !found {
		t.Fatalf("lease manual order = %t, %v", found, err)
	}
	if _, err := queue.ackLeaseWithOwner(
		t.Context(),
		leaseID,
		"worker",
		"session",
	); err != nil {
		t.Fatalf("ack manual order: %v", err)
	}
	requireAutomaticDiscoveryAdmission(t, queue, target, false)
	if duplicate, err := queue.PublishOnce(t.Context(), target, manual); err != nil || !duplicate {
		t.Fatalf("repeat manual order = %t, %v, want duplicate", duplicate, err)
	}
}

func TestAutomaticDiscoveryFailureKeepsOneGlobalRecoveryIntent(t *testing.T) {
	fixture := scriptedQueue(t)
	first := "https://intent-first.example/page"
	second := "https://intent-second.example/page"
	fixture.engine.putErrors[orderBucket] = errors.New("order write failed")
	if _, err := fixture.queue.PublishOnce(
		t.Context(),
		first,
		automaticDiscoveryOrder(first),
	); err == nil {
		t.Fatal("failed first admission was accepted")
	}
	if _, err := fixture.queue.PublishOnce(
		t.Context(),
		second,
		automaticDiscoveryOrder(second),
	); err == nil {
		t.Fatal("second admission bypassed failed intent recovery")
	}
	intents := fixture.engine.buckets[discoveryIntentBucket]
	if len(intents) != 1 {
		t.Fatalf("recovery intents = %d, want 1", len(intents))
	}
	if _, found := intents[first]; !found {
		t.Fatalf("recovery intent keys = %v, want first target", intents)
	}

	delete(fixture.engine.putErrors, orderBucket)
	requireAutomaticDiscoveryAdmission(t, fixture.queue, second, false)
	requireAutomaticDiscoveryAdmission(t, fixture.queue, first, true)
	requireAutomaticDiscoveryAdmission(t, fixture.queue, second, true)
	if remaining := len(fixture.engine.buckets[discoveryIntentBucket]); remaining != 0 {
		t.Fatalf("recovery intents after convergence = %d, want 0", remaining)
	}
}

func TestAutomaticDiscoveryIntentRecoveryWakesSleepingReceiver(t *testing.T) {
	fixture := scriptedQueue(t)
	active := "https://active-intent.example/page"
	requireAutomaticDiscoveryAdmission(t, fixture.queue, active, false)
	if _, _, found, err := fixture.queue.leasePopForSession(
		t.Context(),
		"active-worker",
		"active-session",
	); err != nil || !found {
		t.Fatalf("lease active discovery = %t, %v", found, err)
	}
	select {
	case <-fixture.queue.notify:
	default:
	}
	recovered := "https://recovered-intent.example/page"
	fixture.engine.putErrors[orderBucket] = errors.New("order write failed")
	if _, err := fixture.queue.PublishOnce(
		t.Context(),
		recovered,
		automaticDiscoveryOrder(recovered),
	); err == nil {
		t.Fatal("failed recovery admission was accepted")
	}
	delete(fixture.engine.putErrors, orderBucket)
	requireAutomaticDiscoveryAdmission(t, fixture.queue, active, true)
	select {
	case <-fixture.queue.notify:
	default:
		t.Fatal("recovered automatic discovery did not wake receiver")
	}
	data, _, found, err := fixture.queue.leasePopForSession(
		t.Context(),
		"recovered-worker",
		"recovered-session",
	)
	if err != nil || !found {
		t.Fatalf("lease recovered discovery = %t, %v", found, err)
	}
	order, err := yagocrawlcontract.UnmarshalCrawlOrder(data)
	if err != nil {
		t.Fatalf("decode recovered discovery: %v", err)
	}
	if order.Requests[0].URL != recovered {
		t.Fatalf("recovered discovery = %q, want %q", order.Requests[0].URL, recovered)
	}
}

func TestAutomaticDiscoverySignalsBeforeIntentRelease(t *testing.T) {
	fixture := scriptedQueue(t)
	target := "https://intent-release.example/page"
	fixture.engine.deleteErrors[discoveryIntentBucket] = errors.New("intent delete failed")
	if _, err := fixture.queue.PublishOnce(
		t.Context(),
		target,
		automaticDiscoveryOrder(target),
	); err == nil {
		t.Fatal("intent release failure was hidden")
	}
	select {
	case <-fixture.queue.notify:
	default:
		t.Fatal("committed discovery was not signalled before intent release")
	}
	delete(fixture.engine.deleteErrors, discoveryIntentBucket)
	requireAutomaticDiscoveryAdmission(t, fixture.queue, target, true)
	if _, _, found, err := fixture.queue.leasePopForSession(
		t.Context(),
		"worker",
		"session",
	); err != nil || !found {
		t.Fatalf("lease discovery after intent recovery = %t, %v", found, err)
	}
}

func TestAutomaticDiscoveryRestartSignalsRetainedCompletedIntent(t *testing.T) {
	fixture := scriptedQueue(t)
	target := "https://retained-completed-intent.example/page"
	fixture.engine.deleteErrors[discoveryIntentBucket] = errors.New("intent delete failed")
	if _, err := fixture.queue.PublishOnce(
		t.Context(),
		target,
		automaticDiscoveryOrder(target),
	); err == nil {
		t.Fatal("intent release failure was hidden")
	}
	delete(fixture.engine.deleteErrors, discoveryIntentBucket)

	restartedVault, err := vault.New(fixture.engine)
	if err != nil {
		t.Fatalf("restart vault: %v", err)
	}
	restarted, err := newDurableOrderQueue(restartedVault, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("restart queue: %v", err)
	}
	select {
	case <-restarted.notify:
	default:
		t.Fatal("retained completed discovery intent did not wake restarted receiver")
	}
	if _, _, found, err := restarted.leasePopForSession(
		t.Context(),
		"worker",
		"session",
	); err != nil || !found {
		t.Fatalf("lease retained discovery after restart = %t, %v", found, err)
	}
}

func TestAutomaticDiscoveryNewAdmissionDoesNotScanQueueOrLeases(t *testing.T) {
	fixture := scriptedQueue(t)
	leasedTarget := "https://existing-lease.example/page"
	requireAutomaticDiscoveryAdmission(t, fixture.queue, leasedTarget, false)
	if _, _, found, err := fixture.queue.leasePopForSession(
		t.Context(),
		"worker",
		"session",
	); err != nil || !found {
		t.Fatalf("lease existing discovery = %t, %v", found, err)
	}
	second := automaticDiscoveryOrder("https://existing-pending.example/page")
	if _, err := fixture.queue.PublishOnce(t.Context(), "", second); err != nil {
		t.Fatalf("publish existing pending order: %v", err)
	}
	fixture.engine.scanErrors[automaticOrderIndexBucket] = errors.New("queue scan")
	fixture.engine.scanErrors[leaseBucket] = errors.New("lease scan")

	requireAutomaticDiscoveryAdmission(t, fixture.queue, leasedTarget, true)
	requireAutomaticDiscoveryAdmission(
		t,
		fixture.queue,
		"https://fresh-admission.example/page",
		false,
	)
}

func TestDiscardedAutomaticDiscoveryDuplicateWakesNextOrder(t *testing.T) {
	queue := memQueue(t)
	target := "https://duplicate-ahead.example/page"
	requireAutomaticDiscoveryAdmission(t, queue, target, false)
	data, _, found, err := queue.leasePopForSession(
		t.Context(),
		"owner",
		"owner-session",
	)
	if err != nil || !found {
		t.Fatalf("lease discovery = %t, %v", found, err)
	}
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		sequence, err := queue.enqueueTx(
			tx,
			data,
			yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
		)
		if err != nil {
			return err
		}

		return queue.pendingDiscoveryKeys.Put(tx, orderKey(sequence), []byte(target))
	}); err != nil {
		t.Fatalf("seed duplicate discovery: %v", err)
	}
	normal := testOrder("normal-behind-duplicate")
	if err := queue.Publish(t.Context(), normal); err != nil {
		t.Fatalf("publish normal order: %v", err)
	}
	for {
		select {
		case <-queue.notify:
			continue
		default:
		}

		break
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	leased, err := queue.leaseNext(ctx)
	if err != nil {
		t.Fatalf("lease next order: %v", err)
	}
	order, err := yagocrawlcontract.UnmarshalCrawlOrder(leased)
	if err != nil {
		t.Fatalf("decode next order: %v", err)
	}
	if order.Profile.Handle != normal.Profile.Handle {
		t.Fatalf("next order profile = %q, want %q", order.Profile.Handle, normal.Profile.Handle)
	}
}

func TestAutomaticDiscoveryPartialClaimOrRequeueDoesNotDeliverTwice(t *testing.T) {
	engine := newScriptedEngine()
	queue := reopenAutomaticDiscoveryQueue(t, engine)
	target := "https://dual-ownership.example/page"
	requireAutomaticDiscoveryAdmission(t, queue, target, false)
	data, _, found, err := queue.leasePopForSession(
		t.Context(),
		"worker-one",
		"session-one",
	)
	if err != nil || !found {
		t.Fatalf("lease discovery = %t, %v", found, err)
	}
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		sequence, err := queue.enqueueTx(
			tx,
			data,
			yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
		)
		if err != nil {
			return err
		}

		return queue.pendingDiscoveryKeys.Put(tx, orderKey(sequence), []byte(target))
	}); err != nil {
		t.Fatalf("seed dual ownership: %v", err)
	}
	recovered := reopenAutomaticDiscoveryQueue(t, engine)
	if _, _, found, err := recovered.leasePopForSession(
		t.Context(),
		"worker-two",
		"session-two",
	); err != nil || found {
		t.Fatalf("duplicate delivery = %t, %v, want false, nil", found, err)
	}
	depth, err := recovered.Depth(t.Context())
	if err != nil {
		t.Fatalf("queue depth: %v", err)
	}
	if depth.Pending != 0 || depth.Leased != 1 {
		t.Fatalf("queue depth = %+v, want zero pending and one lease", depth)
	}
	requireAutomaticDiscoveryAdmission(t, recovered, target, true)
}

func requireLegacyAutomaticDiscoveryMigration(t *testing.T, name string, lease bool) {
	t.Helper()
	queue := memQueue(t)
	target := "https://legacy-" + name + ".example/page"
	order := automaticDiscoveryOrder(target)
	data, err := yagocrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal legacy order: %v", err)
	}
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		sequence, err := queue.enqueueTx(tx, data, order.Priority)
		if err != nil {
			return err
		}

		return queue.keys.Put(tx, vault.Key(target), sequence)
	}); err != nil {
		t.Fatalf("seed legacy automatic discovery: %v", err)
	}
	var leaseID string
	if lease {
		_, leaseID, _, err = queue.leasePopForSession(t.Context(), "worker", "session")
		if err != nil {
			t.Fatalf("lease legacy automatic discovery: %v", err)
		}
	}
	requireAutomaticDiscoveryAdmission(t, queue, target, true)
	if !lease {
		return
	}
	record, found := leaseRecordFor(t, queue, leaseID)
	if !found || record.DiscoveryKey != target {
		t.Fatalf("migrated lease = %+v, %t", record, found)
	}
}

func TestLegacyAutomaticDiscoveryPendingMigratesOnRediscovery(t *testing.T) {
	requireLegacyAutomaticDiscoveryMigration(t, "pending", false)
}

func TestLegacyAutomaticDiscoveryLeaseMigratesOnRediscovery(t *testing.T) {
	requireLegacyAutomaticDiscoveryMigration(t, "leased", true)
}

func TestIngestLeaseAuthorizationBindsProfileBeforeRecrawlPersistence(t *testing.T) {
	queue := memQueue(t)
	order := testOrder("owned-profile")
	if err := queue.Publish(t.Context(), order); err != nil {
		t.Fatalf("publish: %v", err)
	}
	_, leaseID, found, err := queue.leasePopForSession(t.Context(), "worker", "session")
	if err != nil || !found {
		t.Fatalf("lease = %t, %v", found, err)
	}
	authorization := leaseAuthorization{
		LeaseID:         leaseID,
		WorkerID:        "worker",
		WorkerSessionID: "session",
		RunID:           hex.EncodeToString(order.Provenance),
		ProfileHandle:   order.Profile.Handle,
	}
	if err := queue.verifyLeaseAuthorization(t.Context(), authorization); err != nil {
		t.Fatalf("owned profile rejected: %v", err)
	}
	authorization.ProfileHandle = "foreign-profile"
	if err := queue.verifyLeaseAuthorization(t.Context(), authorization); err == nil {
		t.Fatal("foreign profile was authorized for current lease")
	}
}

func TestAutomaticDiscoveryWithoutKeyKeepsKeylessQueueSemantics(t *testing.T) {
	queue := memQueue(t)
	order := automaticDiscoveryOrder("https://keyless.example/")
	for range 2 {
		duplicate, err := queue.PublishOnce(context.Background(), "", order)
		if err != nil || duplicate {
			t.Fatalf("keyless automatic discovery = %t, %v", duplicate, err)
		}
	}
}
