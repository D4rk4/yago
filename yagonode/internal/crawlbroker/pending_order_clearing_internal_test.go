package crawlbroker

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestClearPendingOrdersReturnsEmptySuccess(t *testing.T) {
	queue := memQueue(t)

	removed, err := queue.ClearPendingOrders(t.Context())
	if err != nil || removed != 0 {
		t.Fatalf("clear empty queue = %d, %v, want 0, nil", removed, err)
	}
}

func TestClearPendingOrdersStopsAtAnEmptyQueueCutoff(t *testing.T) {
	fixture := scriptedQueue(t)
	queue := fixture.queue
	fixture.engine.viewCount = 0
	fixture.engine.afterViewAt = 2
	fixture.engine.afterView = func() {
		if err := queue.Publish(
			t.Context(),
			testOrder("published-after-empty-cutoff"),
		); err != nil {
			t.Errorf("publish after empty cutoff: %v", err)
		}
	}

	removed, err := queue.ClearPendingOrders(t.Context())
	if err != nil || removed != 0 {
		t.Fatalf("clear empty cutoff = %d, %v, want 0, nil", removed, err)
	}
	depth, err := queue.Depth(t.Context())
	if err != nil || depth.Pending != 1 || depth.Leased != 0 {
		t.Fatalf("queue depth = %+v, %v, want one later order", depth, err)
	}
}

func TestClearPendingOrdersReturnsErrorWhenAlreadyCanceled(t *testing.T) {
	queue := memQueue(t)
	if err := queue.Publish(t.Context(), testOrder("keep")); err != nil {
		t.Fatalf("publish order: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	removed, err := queue.ClearPendingOrders(ctx)
	if !errors.Is(err, context.Canceled) || removed != 0 {
		t.Fatalf("clear canceled queue = %d, %v, want 0 and context canceled", removed, err)
	}
	depth, err := queue.Depth(t.Context())
	if err != nil || depth.Pending != 1 || depth.Leased != 0 {
		t.Fatalf("queue depth after canceled clear = %+v, %v, want one pending", depth, err)
	}
}

func TestClearPendingOrdersStopsWhenIntentRecoveryFails(t *testing.T) {
	queue := memQueue(t)
	if err := queue.Publish(t.Context(), testOrder("keep")); err != nil {
		t.Fatalf("publish order: %v", err)
	}
	if err := queue.persistAutomaticDiscoveryIntent(
		t.Context(),
		"https://intent.example/",
		[]byte("invalid order"),
	); err != nil {
		t.Fatalf("persist invalid recovery intent: %v", err)
	}

	removed, err := queue.ClearPendingOrders(t.Context())
	if err == nil || removed != 0 {
		t.Fatalf("clear failed intent = %d, %v, want 0 and error", removed, err)
	}
	depth, err := queue.Depth(t.Context())
	if err != nil || depth.Pending != 1 || depth.Leased != 0 {
		t.Fatalf("queue depth after intent failure = %+v, %v, want one pending", depth, err)
	}
}

func TestClearPendingOrdersReturnsStorageFailuresWithoutMutation(t *testing.T) {
	failure := errors.New("storage failure")
	cases := []pendingOrderClearStorageFailure{
		{name: "sequence read", readBucket: seqBucket},
		{name: "last key read", scanBucket: orderBucket},
		{name: "page read", pageRead: true},
		{name: "order delete", deleteBucket: orderBucket},
		{name: "normal index delete", deleteBucket: normalOrderIndexBucket},
		{
			name:         "automatic index delete",
			automatic:    true,
			deleteBucket: automaticOrderIndexBucket,
		},
		{name: "pending discovery read", automatic: true, readBucket: pendingDiscoveryKeyBucket},
		{
			name:         "pending discovery delete",
			automatic:    true,
			deleteBucket: pendingDiscoveryKeyBucket,
		},
		{name: "leased discovery read", automatic: true, readBucket: leasedDiscoveryKeyBucket},
		{
			name:                   "unindexed leased discovery read",
			automatic:              true,
			missingPendingIdentity: true,
			readBucket:             leasedDiscoveryKeyBucket,
		},
		{name: "active discovery read", automatic: true, readBucket: activeDiscoveryKeyBucket},
		{
			name:           "legacy discovery read",
			automatic:      true,
			legacyIdentity: true,
			readBucket:     idempotencyBucket,
		},
		{
			name:         "active discovery delete",
			automatic:    true,
			deleteBucket: activeDiscoveryKeyBucket,
		},
		{
			name:           "legacy discovery delete",
			automatic:      true,
			legacyIdentity: true,
			deleteBucket:   idempotencyBucket,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			assertPendingOrderClearFailure(t, test, failure)
		})
	}
}

func TestClearPendingOrdersRejectsSequenceBoundaryBeforeLastOrder(t *testing.T) {
	queue := memQueue(t)
	for _, name := range []string{"first", "second"} {
		if err := queue.Publish(t.Context(), testOrder(name)); err != nil {
			t.Fatalf("publish %s: %v", name, err)
		}
	}
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return queue.seq.Put(tx, seqKey, 1)
	}); err != nil {
		t.Fatalf("corrupt next sequence: %v", err)
	}

	removed, err := queue.ClearPendingOrders(t.Context())
	if err == nil || removed != 0 {
		t.Fatalf("clear corrupt sequence = %d, %v, want 0 and error", removed, err)
	}
	depth, err := queue.Depth(t.Context())
	if err != nil || depth.Pending != 2 || depth.Leased != 0 {
		t.Fatalf("queue depth after rejected clear = %+v, %v, want 2 pending", depth, err)
	}
}

func TestClearPendingOrdersLeavesLeasedOrder(t *testing.T) {
	queue := memQueue(t)
	target := "https://leased.example/"
	if duplicate, err := queue.PublishOnce(
		t.Context(),
		target,
		automaticDiscoveryOrder(target),
	); err != nil || duplicate {
		t.Fatalf("publish automatic order = duplicate %t, %v", duplicate, err)
	}
	for _, name := range []string{"two", "three"} {
		if err := queue.Publish(t.Context(), testOrder(name)); err != nil {
			t.Fatalf("publish %s: %v", name, err)
		}
	}
	_, leaseID, found, err := queue.leasePopForSession(t.Context(), "worker", "session")
	if err != nil || !found {
		t.Fatalf("lease first order = %t, %v", found, err)
	}

	removed, err := queue.ClearPendingOrders(t.Context())
	if err != nil || removed != 2 {
		t.Fatalf("clear queue = %d, %v, want 2, nil", removed, err)
	}
	depth, err := queue.Depth(t.Context())
	if err != nil || depth.Pending != 0 || depth.Leased != 1 {
		t.Fatalf("queue depth = %+v, %v, want 0 pending / 1 leased", depth, err)
	}
	if _, found := leaseRecordFor(t, queue, leaseID); !found {
		t.Fatal("clearing pending orders removed the lease")
	}
	requireAutomaticDiscoveryLeaseIndex(t, queue, target, leaseID, true)
	if duplicate, err := queue.PublishOnce(
		t.Context(),
		target,
		automaticDiscoveryOrder(target),
	); err != nil || !duplicate {
		t.Fatalf("publish while leased = duplicate %t, %v, want true, nil", duplicate, err)
	}
	if _, err := queue.ackLeaseWithOwner(t.Context(), leaseID, "worker", "session"); err != nil {
		t.Fatalf("acknowledge retained lease: %v", err)
	}
	if duplicate, err := queue.PublishOnce(
		t.Context(),
		target,
		automaticDiscoveryOrder(target),
	); err != nil || duplicate {
		t.Fatalf(
			"publish after lease acknowledgment = duplicate %t, %v, want false, nil",
			duplicate,
			err,
		)
	}
}

func TestClearPendingOrdersPreservesLeasedAutomaticIdentity(t *testing.T) {
	for _, test := range []struct {
		name                   string
		removeLeasedKeySidecar bool
	}{
		{name: "leased key present"},
		{
			name:                   "active sequence fallback",
			removeLeasedKeySidecar: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			clearPendingAutomaticDuplicateWithLease(
				t,
				test.removeLeasedKeySidecar,
			)
		})
	}
}

func clearPendingAutomaticDuplicateWithLease(
	t *testing.T,
	removeLeasedKeySidecar bool,
) {
	t.Helper()
	queue := memQueue(t)
	target := "https://leased-duplicate.example/"
	requireAutomaticDiscoveryAdmission(t, queue, target, false)
	_, leaseID, found, err := queue.leasePopForSession(t.Context(), "worker", "session")
	if err != nil || !found {
		t.Fatalf("lease automatic order = %t, %v", found, err)
	}
	lease, found := leaseRecordFor(t, queue, leaseID)
	if !found || lease.DiscoveryKey != target || lease.WorkerID != "worker" ||
		lease.WorkerSessionID != "session" {
		t.Fatalf("automatic lease ownership = %+v, %t", lease, found)
	}
	pendingSequence := seedPendingAutomaticOrderForLeaseClear(
		t,
		queue,
		target,
		lease.DiscoverySequence,
		removeLeasedKeySidecar,
	)
	if (pendingSequence == lease.DiscoverySequence) != !removeLeasedKeySidecar {
		t.Fatalf("pending sequence %d has unexpected relation to leased sequence", pendingSequence)
	}

	removed, err := queue.ClearPendingOrders(t.Context())
	if err != nil || removed != 1 {
		t.Fatalf("clear duplicate pending order = %d, %v, want 1, nil", removed, err)
	}
	depth, err := queue.Depth(t.Context())
	if err != nil || depth.Pending != 0 || depth.Leased != 1 {
		t.Fatalf("queue depth = %+v, %v, want 0 pending / 1 leased", depth, err)
	}
	lease, found = leaseRecordFor(t, queue, leaseID)
	if !found || lease.DiscoveryKey != target || lease.WorkerID != "worker" ||
		lease.WorkerSessionID != "session" {
		t.Fatalf("clearing duplicate changed lease ownership = %+v, %t", lease, found)
	}
	requireAutomaticDiscoveryLeaseIndex(t, queue, target, leaseID, !removeLeasedKeySidecar)
	requireAutomaticClearIdentitySequences(t, queue, target, lease.DiscoverySequence)
	if duplicate, err := queue.PublishOnce(
		t.Context(),
		target,
		automaticDiscoveryOrder(target),
	); err != nil || !duplicate {
		t.Fatalf("publish while lease active = duplicate %t, %v, want true, nil", duplicate, err)
	}
	if _, err := queue.ackLeaseWithOwner(t.Context(), leaseID, "worker", "session"); err != nil {
		t.Fatalf("acknowledge retained lease: %v", err)
	}
	if duplicate, err := queue.PublishOnce(
		t.Context(),
		target,
		automaticDiscoveryOrder(target),
	); err != nil || duplicate {
		t.Fatalf(
			"publish after lease acknowledgment = duplicate %t, %v, want false, nil",
			duplicate,
			err,
		)
	}
}

func seedPendingAutomaticOrderForLeaseClear(
	t *testing.T,
	queue *DurableOrderQueue,
	target string,
	leaseSequence uint64,
	removeLeasedKeySidecar bool,
) uint64 {
	t.Helper()
	data := automaticDiscoveryData(t, target)
	pendingUsesLeaseSequence := !removeLeasedKeySidecar
	var pendingSequence uint64
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		var err error
		pendingSequence, err = seedPendingAutomaticOrderForLeaseClearTx(
			tx,
			queue,
			data,
			leaseSequence,
			pendingUsesLeaseSequence,
		)
		if err != nil {
			return err
		}
		if err := queue.pendingDiscoveryKeys.Put(
			tx,
			orderKey(pendingSequence),
			[]byte(target),
		); err != nil {
			return automaticDiscoveryFixtureError(err)
		}
		if err := queue.keys.Put(tx, vault.Key(target), leaseSequence); err != nil {
			return automaticDiscoveryFixtureError(err)
		}
		if removeLeasedKeySidecar {
			removed, err := queue.leasedDiscoveryKeys.Delete(tx, vault.Key(target))
			if err != nil {
				return automaticDiscoveryFixtureError(err)
			}
			if !removed {
				return fmt.Errorf("leased discovery sidecar was absent before deletion")
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("seed duplicate pending automatic order: %v", err)
	}

	return pendingSequence
}

func seedPendingAutomaticOrderForLeaseClearTx(
	tx *vault.Txn,
	queue *DurableOrderQueue,
	data []byte,
	leaseSequence uint64,
	useLeaseSequence bool,
) (uint64, error) {
	if !useLeaseSequence {
		sequence, err := queue.enqueueTx(
			tx,
			data,
			yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
		)
		if err != nil {
			return 0, automaticDiscoveryFixtureError(err)
		}

		return sequence, nil
	}
	key := orderKey(leaseSequence)
	if err := queue.orders.Put(tx, key, data); err != nil {
		return 0, automaticDiscoveryFixtureError(err)
	}
	if err := queue.automaticOrderIndex.Put(tx, key, priorityIndexMarker); err != nil {
		return 0, automaticDiscoveryFixtureError(err)
	}

	return leaseSequence, nil
}

func requireAutomaticClearIdentitySequences(
	t *testing.T,
	queue *DurableOrderQueue,
	target string,
	wantSequence uint64,
) {
	t.Helper()
	var activeSequence uint64
	var legacySequence uint64
	err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		var active bool
		var err error
		activeSequence, active, err = queue.activeDiscoveryKeys.Get(tx, vault.Key(target))
		if err != nil {
			return automaticDiscoveryFixtureError(err)
		}
		if !active {
			return fmt.Errorf("active discovery identity was removed")
		}
		var legacy bool
		legacySequence, legacy, err = queue.keys.Get(tx, vault.Key(target))
		if err != nil {
			return automaticDiscoveryFixtureError(err)
		}
		if !legacy {
			return fmt.Errorf("legacy discovery identity was removed")
		}

		return nil
	})
	if err != nil {
		t.Fatalf("read discovery identities after clear: %v", err)
	}
	if activeSequence != wantSequence || legacySequence != wantSequence {
		t.Fatalf(
			"discovery sequences after clear = active %d / legacy %d, want %d",
			activeSequence,
			legacySequence,
			wantSequence,
		)
	}
}

func TestClearPendingOrdersPreservesManualIdempotencyAfterAutomaticOrder(t *testing.T) {
	queue := memQueue(t)
	key := "https://shared-idempotency.example/"
	automatic := automaticDiscoveryOrder(key)
	if duplicate, err := queue.PublishOnce(t.Context(), key, automatic); err != nil || duplicate {
		t.Fatalf("publish automatic order = duplicate %t, %v, want false, nil", duplicate, err)
	}
	manual := testOrder("manual-after-automatic")
	if duplicate, err := queue.PublishOnce(t.Context(), key, manual); err != nil || duplicate {
		t.Fatalf("publish manual order = duplicate %t, %v, want false, nil", duplicate, err)
	}

	removed, err := queue.ClearPendingOrders(t.Context())
	if err != nil || removed != 2 {
		t.Fatalf("clear automatic and manual orders = %d, %v, want 2, nil", removed, err)
	}
	if duplicate, err := queue.PublishOnce(t.Context(), key, manual); err != nil || !duplicate {
		t.Fatalf("retry cleared manual order = duplicate %t, %v, want true, nil", duplicate, err)
	}
}

func TestClearPendingOrdersSpansBoundedPages(t *testing.T) {
	queue := memQueue(t)
	publishNumberedOrders(t, queue, 600)

	removed, err := queue.ClearPendingOrders(t.Context())
	if err != nil || removed != 600 {
		t.Fatalf("clear queue = %d, %v, want 600, nil", removed, err)
	}
	depth, err := queue.Depth(t.Context())
	if err != nil || depth.Pending != 0 || depth.Leased != 0 {
		t.Fatalf("queue depth = %+v, %v, want empty", depth, err)
	}
	requirePendingOrderAndPriority(t, queue, 0, false, false)
	requirePendingOrderAndPriority(t, queue, 599, false, false)
}

func TestClearPendingOrdersCountsOnlyCommittedReplayAttempt(t *testing.T) {
	fixture := scriptedQueue(t)
	queue := fixture.queue
	publishNumberedOrders(t, queue, 300)
	fixture.engine.replayNext = true

	removed, err := queue.ClearPendingOrders(t.Context())
	if err != nil || removed != 300 {
		t.Fatalf("clear replayed queue = %d, %v, want 300, nil", removed, err)
	}
	depth, err := queue.Depth(t.Context())
	if err != nil || depth.Pending != 0 || depth.Leased != 0 {
		t.Fatalf("queue depth after replay = %+v, %v, want empty", depth, err)
	}
}

func TestClearPendingOrdersLeavesOrderPublishedAfterFirstBatch(t *testing.T) {
	fixture := scriptedQueue(t)
	queue := fixture.queue
	publishNumberedOrders(t, queue, 600)
	fixture.engine.afterUpdate = func() {
		if err := queue.Publish(
			context.Background(),
			testOrder("published-after-cutoff"),
		); err != nil {
			t.Errorf("publish after clear batch: %v", err)
		}
	}

	removed, err := queue.ClearPendingOrders(t.Context())
	if err != nil || removed != 600 {
		t.Fatalf("clear queue = %d, %v, want 600, nil", removed, err)
	}
	depth, err := queue.Depth(t.Context())
	if err != nil || depth.Pending != 1 || depth.Leased != 0 {
		t.Fatalf("queue depth = %+v, %v, want 1 pending order", depth, err)
	}
	data, _, found, err := queue.leasePopForSession(t.Context(), "worker", "session")
	if err != nil || !found {
		t.Fatalf("lease post-cutoff order = %t, %v", found, err)
	}
	order, err := yagocrawlcontract.UnmarshalCrawlOrder(data)
	if err != nil || order.Profile.Name != "published-after-cutoff" {
		t.Fatalf("post-cutoff order = %q, %v, want published-after-cutoff", order.Profile.Name, err)
	}
}

func TestClearPendingOrdersLeavesFirstOrderPublishedAfterCutoff(t *testing.T) {
	fixture := scriptedQueue(t)
	queue := fixture.queue
	fixture.engine.viewCount = 0
	fixture.engine.afterViewAt = 2
	fixture.engine.afterView = func() {
		if err := queue.Publish(t.Context(), testOrder("published-after-cutoff")); err != nil {
			t.Errorf("publish after cutoff: %v", err)
		}
	}

	removed, err := queue.ClearPendingOrders(t.Context())
	if err != nil || removed != 0 {
		t.Fatalf("clear before first later order = %d, %v, want 0, nil", removed, err)
	}
	depth, err := queue.Depth(t.Context())
	if err != nil || depth.Pending != 1 || depth.Leased != 0 {
		t.Fatalf("queue depth = %+v, %v, want one pending later order", depth, err)
	}
}

func TestClearPendingOrdersCancellationReturnsCommittedCount(t *testing.T) {
	fixture := scriptedQueue(t)
	queue := fixture.queue
	publishNumberedOrders(t, queue, 600)
	ctx, cancel := context.WithCancel(t.Context())
	fixture.engine.afterUpdate = cancel

	removed, err := queue.ClearPendingOrders(ctx)
	if !errors.Is(err, context.Canceled) || removed != 256 {
		t.Fatalf("clear cancelled queue = %d, %v, want 256 and context canceled", removed, err)
	}
	depth, err := queue.Depth(t.Context())
	if err != nil || depth.Pending != 344 || depth.Leased != 0 {
		t.Fatalf("queue depth after cancellation = %+v, %v, want 344 pending", depth, err)
	}
	requirePendingOrderAndPriority(t, queue, 0, false, false)
	requirePendingOrderAndPriority(t, queue, 256, true, false)
	removed, err = queue.ClearPendingOrders(t.Context())
	if err != nil || removed != 344 {
		t.Fatalf("retry clear = %d, %v, want 344, nil", removed, err)
	}
}

func TestClearPendingOrdersRollsBackFailingBatchAndReportsCommittedCount(t *testing.T) {
	queue := memQueue(t)
	publishNumberedOrders(t, queue, 600)
	validValue := pendingOrderPayload(t, queue, 257)
	replacePendingOrderPayload(t, queue, 257, []byte("invalid order"))

	removed, err := queue.ClearPendingOrders(t.Context())
	if err == nil || removed != 256 {
		t.Fatalf("clear corrupt queue = %d, %v, want 256 and error", removed, err)
	}
	depth, err := queue.Depth(t.Context())
	if err != nil || depth.Pending != 344 || depth.Leased != 0 {
		t.Fatalf("queue depth after failed batch = %+v, %v, want 344 pending", depth, err)
	}
	requirePendingOrderAndPriority(t, queue, 0, false, false)
	requirePendingOrderAndPriority(t, queue, 256, true, false)
	requirePendingOrderAndPriority(t, queue, 257, true, false)
	replacePendingOrderPayload(t, queue, 257, validValue)
	removed, err = queue.ClearPendingOrders(t.Context())
	if err != nil || removed != 344 {
		t.Fatalf("retry clear = %d, %v, want 344, nil", removed, err)
	}
}

func TestClearPendingOrdersReconcilesAutomaticIntentAndPermitsReseed(t *testing.T) {
	fixture := scriptedQueue(t)
	queue := fixture.queue
	target := "https://discovery.example/"
	data, err := yagocrawlcontract.MarshalCrawlOrder(automaticDiscoveryOrder(target))
	if err != nil {
		t.Fatalf("marshal automatic order: %v", err)
	}
	if err := queue.persistAutomaticDiscoveryIntent(t.Context(), target, data); err != nil {
		t.Fatalf("persist automatic intent: %v", err)
	}

	removed, err := queue.ClearPendingOrders(t.Context())
	if err != nil || removed != 1 {
		t.Fatalf("clear automatic queue = %d, %v, want 1, nil", removed, err)
	}
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		if _, found, err := queue.activeDiscoveryKeys.Get(tx, vault.Key(target)); err != nil {
			return fmt.Errorf("read active discovery identity: %w", err)
		} else if found {
			t.Fatal("cleared automatic order retained active discovery identity")
		}
		if _, found, err := queue.pendingDiscoveryKeys.Get(tx, orderKey(0)); err != nil {
			return fmt.Errorf("read pending discovery identity: %w", err)
		} else if found {
			t.Fatal("cleared automatic order retained pending discovery identity")
		}
		if _, found, err := queue.discoveryIntents.Get(tx, vault.Key(target)); err != nil {
			return fmt.Errorf("read discovery intent: %w", err)
		} else if found {
			t.Fatal("cleared automatic order retained discovery intent")
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect cleared automatic state: %v", err)
	}
	requirePendingOrderAndPriority(t, queue, 0, false, false)

	reopenedStorage, err := vault.New(fixture.engine)
	if err != nil {
		t.Fatalf("reopen queue storage: %v", err)
	}
	reopened, err := newDurableOrderQueue(reopenedStorage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("reopen cleared queue: %v", err)
	}
	depth, err := reopened.Depth(t.Context())
	if err != nil || depth.Pending != 0 {
		t.Fatalf("reopened queue depth = %+v, %v, want empty", depth, err)
	}
	duplicate, err := reopened.PublishOnce(
		t.Context(),
		target,
		automaticDiscoveryOrder(target),
	)
	if err != nil || duplicate {
		t.Fatalf("publish cleared seed = duplicate %t, %v, want false, nil", duplicate, err)
	}
}

func TestClearPendingOrdersAcceptsIndexedAutomaticIdentityOutsideURLs(t *testing.T) {
	queue := memQueue(t)
	identity := "combined-search-seed"
	order := automaticDiscoveryOrder("https://first.example/")
	order.Requests = append(order.Requests, yagocrawlcontract.CrawlRequest{
		URL: "https://second.example/",
	})
	if duplicate, err := queue.PublishOnce(t.Context(), identity, order); err != nil || duplicate {
		t.Fatalf("publish indexed automatic order = duplicate %t, %v", duplicate, err)
	}

	removed, err := queue.ClearPendingOrders(t.Context())
	if err != nil || removed != 1 {
		t.Fatalf("clear indexed automatic order = %d, %v, want 1, nil", removed, err)
	}
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		if _, found, err := queue.pendingDiscoveryKeys.Get(tx, orderKey(0)); err != nil {
			return fmt.Errorf("read pending discovery identity: %w", err)
		} else if found {
			t.Fatal("clear retained the pending automatic identity")
		}
		if _, found, err := queue.activeDiscoveryKeys.Get(tx, vault.Key(identity)); err != nil {
			return fmt.Errorf("read active discovery identity: %w", err)
		} else if found {
			t.Fatal("clear retained the active automatic identity")
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect indexed automatic identity: %v", err)
	}
	duplicate, err := queue.PublishOnce(t.Context(), identity, order)
	if err != nil || duplicate {
		t.Fatalf("republish cleared identity = duplicate %t, %v, want false, nil", duplicate, err)
	}
}

func TestClearPendingOrdersRejectsEmptyAutomaticDiscoveryIdentity(t *testing.T) {
	queue := memQueue(t)
	target := "https://empty-identity.example/"
	if duplicate, err := queue.PublishOnce(
		t.Context(),
		target,
		automaticDiscoveryOrder(target),
	); err != nil || duplicate {
		t.Fatalf("publish automatic order = duplicate %t, %v", duplicate, err)
	}
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return queue.pendingDiscoveryKeys.Put(tx, orderKey(0), []byte{})
	}); err != nil {
		t.Fatalf("corrupt pending discovery identity: %v", err)
	}

	removed, err := queue.ClearPendingOrders(t.Context())
	if err == nil || removed != 0 {
		t.Fatalf("clear empty discovery identity = %d, %v, want 0 and error", removed, err)
	}
	depth, err := queue.Depth(t.Context())
	if err != nil || depth.Pending != 1 || depth.Leased != 0 {
		t.Fatalf("queue depth after rejected clear = %+v, %v, want 1 pending", depth, err)
	}
	requirePendingOrderAndPriority(t, queue, 0, true, true)
}

func TestClearPendingOrdersPreservesManualIdempotencyAndRemovesLegacyAutoKey(t *testing.T) {
	queue := memQueue(t)
	if duplicate, err := queue.PublishOnce(
		t.Context(),
		"manual-key",
		testOrder("manual"),
	); err != nil || duplicate {
		t.Fatalf("publish manual order = duplicate %t, %v", duplicate, err)
	}
	target := "https://legacy.example/"
	order := automaticDiscoveryOrder(target)
	order.Requests = append(order.Requests, yagocrawlcontract.CrawlRequest{
		URL: "https://legacy-sibling.example/",
	})
	data, err := yagocrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal legacy automatic order: %v", err)
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
		return queue.keys.Put(tx, vault.Key(target), sequence)
	}); err != nil {
		t.Fatalf("seed legacy automatic key: %v", err)
	}

	removed, err := queue.ClearPendingOrders(t.Context())
	if err != nil || removed != 2 {
		t.Fatalf("clear queue = %d, %v, want 2, nil", removed, err)
	}
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		if _, found, err := queue.keys.Get(tx, vault.Key("manual-key")); err != nil {
			return fmt.Errorf("read manual idempotency key: %w", err)
		} else if !found {
			t.Fatal("clear removed a manual idempotency key")
		}
		if _, found, err := queue.keys.Get(tx, vault.Key(target)); err != nil {
			return fmt.Errorf("read legacy automatic key: %w", err)
		} else if found {
			t.Fatal("clear retained a legacy automatic-discovery key")
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect idempotency keys: %v", err)
	}
	requirePendingOrderAndPriority(t, queue, 0, false, false)
	requirePendingOrderAndPriority(t, queue, 1, false, false)
	duplicate, err := queue.PublishOnce(t.Context(), "manual-key", testOrder("retry"))
	if err != nil || !duplicate {
		t.Fatalf("manual retry = duplicate %t, %v, want true, nil", duplicate, err)
	}
}

func requirePendingOrderAndPriority(
	t *testing.T,
	queue *DurableOrderQueue,
	sequence uint64,
	wantOrder bool,
	wantAutomatic bool,
) {
	t.Helper()
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, orderFound, err := queue.orders.Get(tx, orderKey(sequence))
		if err != nil {
			return fmt.Errorf("read pending order: %w", err)
		}
		_, normalFound, err := queue.normalOrderIndex.Get(tx, orderKey(sequence))
		if err != nil {
			return fmt.Errorf("read normal order index: %w", err)
		}
		_, automaticFound, err := queue.automaticOrderIndex.Get(tx, orderKey(sequence))
		if err != nil {
			return fmt.Errorf("read automatic order index: %w", err)
		}
		if orderFound != wantOrder || normalFound != (wantOrder && !wantAutomatic) ||
			automaticFound != (wantOrder && wantAutomatic) {
			t.Fatalf(
				"sequence %d order/index state = %t/%t/%t, want %t/%t/%t",
				sequence,
				orderFound,
				normalFound,
				automaticFound,
				wantOrder,
				wantOrder && !wantAutomatic,
				wantOrder && wantAutomatic,
			)
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect sequence %d: %v", sequence, err)
	}
}

type pendingOrderClearStorageFailure struct {
	name                   string
	automatic              bool
	legacyIdentity         bool
	missingPendingIdentity bool
	readBucket             vault.Name
	deleteBucket           vault.Name
	scanBucket             vault.Name
	pageRead               bool
}

func assertPendingOrderClearFailure(
	t *testing.T,
	test pendingOrderClearStorageFailure,
	failure error,
) {
	t.Helper()
	fixture := scriptedQueue(t)
	queue := fixture.queue
	fixture.engine.transactional = true
	target := "https://clear-failure.example/"
	preparePendingOrderClearFailure(t, queue, test, target)
	applyPendingOrderClearFailure(fixture.engine, test, failure)

	removed, err := queue.ClearPendingOrders(t.Context())
	if !errors.Is(err, failure) || removed != 0 {
		t.Fatalf("clear with storage failure = %d, %v, want 0 and storage error", removed, err)
	}
	clearPendingOrderClearFailure(fixture.engine, test)
	depth, err := queue.Depth(t.Context())
	if err != nil || depth.Pending != 1 || depth.Leased != 0 {
		t.Fatalf("queue after storage failure = %+v, %v, want one pending", depth, err)
	}
	requirePendingOrderAndPriority(t, queue, 0, true, test.automatic)
	if test.automatic {
		requireAutomaticPendingIdentityState(
			t,
			queue,
			vault.Key(target),
			!test.missingPendingIdentity,
			test.legacyIdentity,
		)
	}
}

func preparePendingOrderClearFailure(
	t *testing.T,
	queue *DurableOrderQueue,
	test pendingOrderClearStorageFailure,
	target string,
) {
	t.Helper()
	if !test.automatic {
		if err := queue.Publish(t.Context(), testOrder("clear-failure")); err != nil {
			t.Fatalf("publish normal order: %v", err)
		}
		return
	}
	if duplicate, err := queue.PublishOnce(
		t.Context(),
		target,
		automaticDiscoveryOrder(target),
	); err != nil || duplicate {
		t.Fatalf("publish automatic order = duplicate %t, %v", duplicate, err)
	}
	if test.missingPendingIdentity {
		if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			if _, err := queue.pendingDiscoveryKeys.Delete(tx, orderKey(0)); err != nil {
				return fmt.Errorf("remove pending identity: %w", err)
			}
			return nil
		}); err != nil {
			t.Fatalf("remove pending identity: %v", err)
		}
	}
	if test.legacyIdentity {
		if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			if err := queue.keys.Put(tx, vault.Key(target), 0); err != nil {
				return fmt.Errorf("seed legacy identity: %w", err)
			}
			return nil
		}); err != nil {
			t.Fatalf("seed legacy identity: %v", err)
		}
	}
}

func applyPendingOrderClearFailure(
	engine *scriptedEngine,
	test pendingOrderClearStorageFailure,
	failure error,
) {
	if test.readBucket != "" {
		engine.readErrors[test.readBucket] = failure
	}
	if test.deleteBucket != "" {
		engine.deleteErrors[test.deleteBucket] = failure
	}
	if test.scanBucket != "" {
		engine.scanErrors[test.scanBucket] = failure
	}
	if test.pageRead {
		engine.pageErrors[orderBucket] = failure
	}
}

func clearPendingOrderClearFailure(engine *scriptedEngine, test pendingOrderClearStorageFailure) {
	delete(engine.readErrors, test.readBucket)
	delete(engine.deleteErrors, test.deleteBucket)
	delete(engine.scanErrors, test.scanBucket)
	delete(engine.pageErrors, orderBucket)
}

func requireAutomaticPendingIdentityState(
	t *testing.T,
	queue *DurableOrderQueue,
	target vault.Key,
	wantPending bool,
	wantLegacy bool,
) {
	t.Helper()
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, pending, err := queue.pendingDiscoveryKeys.Get(tx, orderKey(0))
		if err != nil {
			return fmt.Errorf("read pending identity: %w", err)
		}
		if pending != wantPending {
			t.Fatalf("pending identity present = %t, want %t", pending, wantPending)
		}
		if _, found, err := queue.activeDiscoveryKeys.Get(tx, target); err != nil {
			return fmt.Errorf("read active identity: %w", err)
		} else if !found {
			t.Fatal("storage failure removed active discovery identity")
		}
		_, legacy, err := queue.keys.Get(tx, target)
		if err != nil {
			return fmt.Errorf("read legacy identity: %w", err)
		}
		if legacy != wantLegacy {
			t.Fatalf("legacy identity present = %t, want %t", legacy, wantLegacy)
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect automatic state after storage failure: %v", err)
	}
}

func TestClearPendingOrdersRejectsMalformedOrderKey(t *testing.T) {
	queue := memQueue(t)
	if err := queue.Publish(t.Context(), testOrder("valid")); err != nil {
		t.Fatalf("publish valid order: %v", err)
	}
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		data, err := yagocrawlcontract.MarshalCrawlOrder(testOrder("malformed-key"))
		if err != nil {
			return fmt.Errorf("marshal malformed-key order: %w", err)
		}
		if err := queue.orders.Put(tx, vault.Key{0}, data); err != nil {
			return fmt.Errorf("store malformed-key order: %w", err)
		}
		return nil
	}); err != nil {
		t.Fatalf("store malformed order key: %v", err)
	}

	removed, err := queue.ClearPendingOrders(t.Context())
	if err == nil || removed != 0 {
		t.Fatalf("clear malformed key = %d, %v, want 0 and error", removed, err)
	}
	depth, err := queue.Depth(t.Context())
	if err != nil || depth.Pending != 2 || depth.Leased != 0 {
		t.Fatalf("queue depth = %+v, %v, want 2 pending", depth, err)
	}
}

func TestClearPendingOrdersRejectsMalformedLastOrderKey(t *testing.T) {
	queue := memQueue(t)
	if err := queue.Publish(t.Context(), testOrder("valid")); err != nil {
		t.Fatalf("publish valid order: %v", err)
	}
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		data, err := yagocrawlcontract.MarshalCrawlOrder(testOrder("malformed-last"))
		if err != nil {
			return fmt.Errorf("marshal malformed-last order: %w", err)
		}
		if err := queue.orders.Put(tx, vault.Key("z-malformed"), data); err != nil {
			return fmt.Errorf("store malformed-last order: %w", err)
		}
		return nil
	}); err != nil {
		t.Fatalf("store malformed last key: %v", err)
	}

	removed, err := queue.ClearPendingOrders(t.Context())
	if err == nil || removed != 0 {
		t.Fatalf("clear malformed last key = %d, %v, want 0 and error", removed, err)
	}
	depth, err := queue.Depth(t.Context())
	if err != nil || depth.Pending != 2 || depth.Leased != 0 {
		t.Fatalf("queue depth = %+v, %v, want 2 pending", depth, err)
	}
}

func publishNumberedOrders(t *testing.T, queue *DurableOrderQueue, quantity int) {
	t.Helper()
	for index := range quantity {
		if err := queue.Publish(t.Context(), testOrder("order-"+strconv.Itoa(index))); err != nil {
			t.Fatalf("publish order %d: %v", index, err)
		}
	}
}

func pendingOrderPayload(t *testing.T, queue *DurableOrderQueue, sequence uint64) []byte {
	t.Helper()
	var payload []byte
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		value, found, err := queue.orders.Get(tx, orderKey(sequence))
		if err != nil {
			return fmt.Errorf("read pending order %d: %w", sequence, err)
		}
		if !found {
			t.Fatalf("pending order %d missing", sequence)
		}
		payload = value
		return nil
	}); err != nil {
		t.Fatalf("read pending order %d: %v", sequence, err)
	}
	return payload
}

func replacePendingOrderPayload(
	t *testing.T,
	queue *DurableOrderQueue,
	sequence uint64,
	payload []byte,
) {
	t.Helper()
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := queue.orders.Put(tx, orderKey(sequence), payload); err != nil {
			return fmt.Errorf("write pending order %d: %w", sequence, err)
		}
		return nil
	}); err != nil {
		t.Fatalf("write pending order %d: %v", sequence, err)
	}
}
