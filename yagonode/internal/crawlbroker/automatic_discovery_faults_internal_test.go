package crawlbroker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type automaticDiscoveryTransactionEngine struct {
	*scriptedEngine
	viewFailureAfter   int
	updateFailureAfter int
	gateMu             sync.Mutex
	gateNextUpdate     bool
	updateReached      chan struct{}
	updateRelease      chan struct{}
}

func (e *automaticDiscoveryTransactionEngine) View(
	ctx context.Context,
	fn func(vault.EngineTxn) error,
) error {
	if e.viewFailureAfter > 0 {
		e.viewFailureAfter--
		if e.viewFailureAfter == 0 {
			return errors.New("view failed")
		}
	}

	return e.scriptedEngine.View(ctx, fn)
}

func (e *automaticDiscoveryTransactionEngine) Update(
	ctx context.Context,
	fn func(vault.EngineTxn) error,
) error {
	e.gateMu.Lock()
	gated := e.gateNextUpdate
	reached := e.updateReached
	release := e.updateRelease
	e.gateNextUpdate = false
	e.gateMu.Unlock()
	if gated {
		close(reached)
		select {
		case <-release:
		case <-ctx.Done():
			return fmt.Errorf("wait for automatic discovery update: %w", ctx.Err())
		}
	}
	if e.updateFailureAfter > 0 {
		e.updateFailureAfter--
		if e.updateFailureAfter == 0 {
			return errors.New("update failed")
		}
	}

	return e.scriptedEngine.Update(ctx, fn)
}

func automaticDiscoveryFixtureError(err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("automatic discovery fixture: %w", err)
}

func (e *automaticDiscoveryTransactionEngine) gateNext() (<-chan struct{}, chan<- struct{}) {
	e.gateMu.Lock()
	defer e.gateMu.Unlock()
	e.gateNextUpdate = true
	e.updateReached = make(chan struct{})
	e.updateRelease = make(chan struct{})

	return e.updateReached, e.updateRelease
}

func automaticDiscoveryTransactionQueue(
	t *testing.T,
) (*DurableOrderQueue, *automaticDiscoveryTransactionEngine) {
	t.Helper()
	engine := &automaticDiscoveryTransactionEngine{scriptedEngine: newScriptedEngine()}
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("open fault storage: %v", err)
	}
	queue, err := newDurableOrderQueue(storage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("open fault queue: %v", err)
	}

	return queue, engine
}

func seedTrackedAutomaticDiscovery(
	t *testing.T,
	queue *DurableOrderQueue,
	target string,
) uint64 {
	t.Helper()
	order := automaticDiscoveryOrder(target)
	data := automaticDiscoveryData(t, target)
	var sequence uint64
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		var enqueueErr error
		sequence, enqueueErr = queue.enqueueTx(tx, data, order.Priority)
		if enqueueErr != nil {
			return enqueueErr
		}
		if err := queue.activeDiscoveryKeys.Put(
			tx,
			vault.Key(target),
			sequence,
		); err != nil {
			return automaticDiscoveryFixtureError(err)
		}

		return queue.pendingDiscoveryKeys.Put(
			tx,
			orderKey(sequence),
			[]byte(target),
		)
	}); err != nil {
		t.Fatalf("seed tracked automatic discovery: %v", err)
	}

	return sequence
}

func automaticDiscoveryData(t *testing.T, target string) []byte {
	t.Helper()
	data, err := yagocrawlcontract.MarshalCrawlOrder(automaticDiscoveryOrder(target))
	if err != nil {
		t.Fatalf("marshal automatic discovery: %v", err)
	}

	return data
}

func seedAutomaticDiscoveryLease(
	t *testing.T,
	queue *DurableOrderQueue,
	leaseID string,
	record leaseRecord,
	indexKey string,
) {
	t.Helper()
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := queue.leases.Put(tx, vault.Key(leaseID), record); err != nil {
			return automaticDiscoveryFixtureError(err)
		}
		if indexKey == "" {
			return nil
		}

		return queue.leasedDiscoveryKeys.Put(
			tx,
			vault.Key(indexKey),
			[]byte(leaseID),
		)
	}); err != nil {
		t.Fatalf("seed automatic discovery lease: %v", err)
	}
}

func automaticDiscoveryOutstandingForTest(
	t *testing.T,
	queue *DurableOrderQueue,
	target string,
	migrate bool,
) (bool, error) {
	t.Helper()
	var outstanding bool
	err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		var lookupErr error
		outstanding, lookupErr = queue.automaticDiscoveryOutstandingTx(
			tx,
			target,
			migrate,
		)

		return lookupErr
	})

	return outstanding, automaticDiscoveryFixtureError(err)
}

func TestAutomaticDiscoveryOutstandingFindsTrackedOrder(t *testing.T) {
	fixture := scriptedQueue(t)
	target := "https://tracked-outstanding.example/"
	seedTrackedAutomaticDiscovery(t, fixture.queue, target)
	outstanding, err := automaticDiscoveryOutstandingForTest(
		t,
		fixture.queue,
		target,
		false,
	)
	if err != nil || !outstanding {
		t.Fatalf("tracked automatic discovery = %t, %v", outstanding, err)
	}
}

func duplicatePendingAutomaticDiscovery(
	t *testing.T,
	fixture scriptedQueueFixture,
	target string,
) pendingOrderHead {
	t.Helper()
	data := automaticDiscoveryData(t, target)
	key := orderKey(7)
	record := leaseRecord{
		OrderData:         data,
		DiscoveryKey:      target,
		DiscoverySequence: 1,
	}
	if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := fixture.queue.orders.Put(tx, key, data); err != nil {
			return automaticDiscoveryFixtureError(err)
		}
		if err := fixture.queue.automaticOrderIndex.Put(
			tx,
			key,
			priorityIndexMarker,
		); err != nil {
			return automaticDiscoveryFixtureError(err)
		}
		if err := fixture.queue.pendingDiscoveryKeys.Put(
			tx,
			key,
			[]byte(target),
		); err != nil {
			return automaticDiscoveryFixtureError(err)
		}
		if err := fixture.queue.activeDiscoveryKeys.Put(
			tx,
			vault.Key(target),
			record.DiscoverySequence,
		); err != nil {
			return automaticDiscoveryFixtureError(err)
		}
		if err := fixture.queue.leases.Put(tx, vault.Key("lease"), record); err != nil {
			return automaticDiscoveryFixtureError(err)
		}

		return fixture.queue.leasedDiscoveryKeys.Put(
			tx,
			vault.Key(target),
			[]byte("lease"),
		)
	}); err != nil {
		t.Fatalf("seed duplicate pending discovery: %v", err)
	}

	return pendingOrderHead{
		index:     fixture.queue.automaticOrderIndex,
		key:       key,
		data:      data,
		found:     true,
		automatic: true,
	}
}

func untrackedAutomaticDiscoveryForTest(
	t *testing.T,
	queue *DurableOrderQueue,
	target string,
	migrate bool,
) (bool, error) {
	t.Helper()
	var outstanding bool
	err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		var lookupErr error
		outstanding, lookupErr = queue.untrackedAutomaticDiscoveryTx(
			tx,
			target,
			migrate,
		)

		return lookupErr
	})

	return outstanding, automaticDiscoveryFixtureError(err)
}

func TestAutomaticDiscoveryPublishPropagatesTransactionFailures(t *testing.T) {
	tests := []struct {
		name        string
		seed        bool
		failViews   int
		failUpdates int
	}{
		{name: "growth read", failViews: 2},
		{name: "duplicate repair", seed: true, failUpdates: 1},
		{name: "intent persistence", failUpdates: 1},
		{name: "intent completion", failUpdates: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queue, engine := automaticDiscoveryTransactionQueue(t)
			target := "https://" + test.name + ".example/"
			if test.seed {
				seedTrackedAutomaticDiscovery(t, queue, target)
			}
			engine.viewFailureAfter = test.failViews
			engine.updateFailureAfter = test.failUpdates
			if _, err := queue.PublishOnce(
				t.Context(),
				target,
				automaticDiscoveryOrder(target),
			); err == nil {
				t.Fatal("automatic discovery transaction failure was hidden")
			}
		})
	}
}

func TestAutomaticDiscoveryGrowthAdmissionHonorsStoragePressure(t *testing.T) {
	t.Run("pressure", func(t *testing.T) {
		queue := memQueue(t)
		queue.growthAdmission = &scriptedGrowthAdmission{err: errors.New("pressure")}
		if _, err := queue.admitAutomaticDiscoveryGrowth(
			t.Context(),
			"https://pressure.example/",
		); err == nil {
			t.Fatal("fresh automatic discovery bypassed storage pressure")
		}
	})
	t.Run("accepted", func(t *testing.T) {
		queue := memQueue(t)
		admission := &scriptedGrowthAdmission{}
		queue.growthAdmission = admission
		duplicate, err := queue.admitAutomaticDiscoveryGrowth(
			t.Context(),
			"https://accepted.example/",
		)
		if err != nil || duplicate {
			t.Fatalf("accepted automatic discovery = %t, %v", duplicate, err)
		}
		if admission.calls != 1 {
			t.Fatalf("growth checks = %d, want 1", admission.calls)
		}
	})
}

func TestAutomaticDiscoveryReleasedOwnershipRechecksGrowthAdmission(t *testing.T) {
	queue, engine := automaticDiscoveryTransactionQueue(t)
	target := "https://released-under-pressure.example/"
	requireAutomaticDiscoveryAdmission(t, queue, target, false)
	_, leaseID, found, err := queue.leasePopForSession(
		t.Context(),
		"worker",
		"session",
	)
	if err != nil || !found {
		t.Fatalf("lease active discovery = %t, %v", found, err)
	}
	pressure := &scriptedGrowthAdmission{err: errors.New("pressure")}
	queue.growthAdmission = pressure
	reached, release := engine.gateNext()
	result := make(chan error, 1)
	go func() {
		_, publishErr := queue.PublishOnce(
			t.Context(),
			target,
			automaticDiscoveryOrder(target),
		)
		result <- publishErr
	}()
	<-reached
	if _, err := queue.ackLeaseWithOwner(
		t.Context(),
		leaseID,
		"worker",
		"session",
	); err != nil {
		release <- struct{}{}
		t.Fatalf("release active discovery: %v", err)
	}
	release <- struct{}{}
	if err := <-result; err == nil {
		t.Fatal("released discovery bypassed storage pressure")
	}
	if pressure.calls != 1 {
		t.Fatalf("growth checks = %d, want 1", pressure.calls)
	}
	depth, err := queue.Depth(t.Context())
	if err != nil {
		t.Fatalf("queue depth: %v", err)
	}
	if depth.Pending != 0 || depth.Leased != 0 {
		t.Fatalf("queue depth after rejected rediscovery = %+v", depth)
	}
}

func TestAutomaticDiscoveryRejectsCorruptOwnershipEvidence(t *testing.T) {
	target := "https://corrupt-ownership.example/"
	t.Run("active sequence", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.buckets[activeDiscoveryKeyBucket][target] = []byte{1}
		if _, err := automaticDiscoveryOutstandingForTest(
			t,
			fixture.queue,
			target,
			false,
		); err == nil {
			t.Fatal("corrupt active sequence was accepted")
		}
	})
	t.Run("active order", func(t *testing.T) {
		fixture := scriptedQueue(t)
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.activeDiscoveryKeys.Put(tx, vault.Key(target), 0)
		}); err != nil {
			t.Fatalf("seed active sequence: %v", err)
		}
		fixture.engine.buckets[orderBucket][string(orderKey(0))] = []byte("{")
		if _, err := automaticDiscoveryOutstandingForTest(
			t,
			fixture.queue,
			target,
			false,
		); err == nil {
			t.Fatal("corrupt active order was accepted")
		}
	})
	t.Run("legacy sequence", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.buckets[idempotencyBucket][target] = []byte{1}
		if _, err := automaticDiscoveryOutstandingForTest(
			t,
			fixture.queue,
			target,
			false,
		); err == nil {
			t.Fatal("corrupt legacy sequence was accepted")
		}
	})
	t.Run("legacy order", func(t *testing.T) {
		fixture := scriptedQueue(t)
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.keys.Put(tx, vault.Key(target), 0)
		}); err != nil {
			t.Fatalf("seed legacy sequence: %v", err)
		}
		fixture.engine.buckets[orderBucket][string(orderKey(0))] = []byte("{")
		if _, err := automaticDiscoveryOutstandingForTest(
			t,
			fixture.queue,
			target,
			false,
		); err == nil {
			t.Fatal("corrupt legacy order was accepted")
		}
	})
}

func TestAutomaticDiscoveryRejectsCorruptLeaseEvidence(t *testing.T) {
	target := "https://corrupt-ownership.example/"
	t.Run("indexed lease", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.buckets[leasedDiscoveryKeyBucket][target] = []byte("lease")
		fixture.engine.buckets[leaseBucket]["lease"] = []byte("{")
		if _, err := untrackedAutomaticDiscoveryForTest(
			t,
			fixture.queue,
			target,
			true,
		); err == nil {
			t.Fatal("corrupt indexed lease was accepted")
		}
	})
	t.Run("lease order", func(t *testing.T) {
		fixture := scriptedQueue(t)
		seedAutomaticDiscoveryLease(
			t,
			fixture.queue,
			"unindexed-lease",
			leaseRecord{OrderData: []byte("{")},
			"",
		)
		if _, err := untrackedAutomaticDiscoveryForTest(
			t,
			fixture.queue,
			target,
			true,
		); err == nil {
			t.Fatal("corrupt lease order was accepted")
		}
	})
	t.Run("indexed lease order", func(t *testing.T) {
		fixture := scriptedQueue(t)
		seedAutomaticDiscoveryLease(
			t,
			fixture.queue,
			"lease",
			leaseRecord{OrderData: []byte("{")},
			target,
		)
		if _, err := untrackedAutomaticDiscoveryForTest(
			t,
			fixture.queue,
			target,
			true,
		); err == nil {
			t.Fatal("corrupt indexed lease order was accepted")
		}
	})
	t.Run("untracked lease lookup", func(t *testing.T) {
		fixture := scriptedQueue(t)
		seedAutomaticDiscoveryLease(
			t,
			fixture.queue,
			"lease",
			leaseRecord{
				OrderData:    automaticDiscoveryData(t, target),
				DiscoveryKey: target,
			},
			"",
		)
		outstanding, err := untrackedAutomaticDiscoveryForTest(
			t,
			fixture.queue,
			target,
			false,
		)
		if err != nil || !outstanding {
			t.Fatalf("untracked lease lookup = %t, %v", outstanding, err)
		}
	})
}

func TestAutomaticDiscoveryOrderMatchingRejectsOtherOrders(t *testing.T) {
	if _, err := automaticDiscoveryOrderMatchesKey([]byte("{"), "target"); err == nil {
		t.Fatal("malformed order was accepted")
	}
	normal, err := yagocrawlcontract.MarshalCrawlOrder(testOrder("normal"))
	if err != nil {
		t.Fatalf("marshal normal order: %v", err)
	}
	if matched, err := automaticDiscoveryOrderMatchesKey(normal, "target"); err != nil || matched {
		t.Fatalf("normal order match = %t, %v", matched, err)
	}
	if matched, err := automaticDiscoveryOrderMatchesKey(
		automaticDiscoveryData(t, "https://other.example/"),
		"target",
	); err != nil || matched {
		t.Fatalf("other URL match = %t, %v", matched, err)
	}
}

func TestAutomaticDiscoveryPendingRecoveryBoundaries(t *testing.T) {
	target := "https://pending-recovery.example/"
	t.Run("scan failure", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.scanErrors[automaticOrderIndexBucket] = errors.New("scan failed")
		if _, err := untrackedAutomaticDiscoveryForTest(
			t,
			fixture.queue,
			target,
			true,
		); err == nil {
			t.Fatal("pending scan failure was hidden")
		}
	})
	t.Run("dangling index", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.buckets[automaticOrderIndexBucket][string(orderKey(0))] = []byte{1}
		if outstanding, err := untrackedAutomaticDiscoveryForTest(
			t,
			fixture.queue,
			target,
			true,
		); err != nil || outstanding {
			t.Fatalf("dangling pending index = %t, %v", outstanding, err)
		}
	})
	t.Run("malformed order", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.buckets[automaticOrderIndexBucket][string(orderKey(0))] = []byte{1}
		fixture.engine.buckets[orderBucket][string(orderKey(0))] = []byte("{")
		if _, err := untrackedAutomaticDiscoveryForTest(
			t,
			fixture.queue,
			target,
			true,
		); err == nil {
			t.Fatal("malformed pending order was accepted")
		}
	})
	t.Run("other URL", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.buckets[automaticOrderIndexBucket][string(orderKey(0))] = []byte{1}
		fixture.engine.buckets[orderBucket][string(orderKey(0))] = automaticDiscoveryData(
			t,
			"https://other.example/",
		)
		if outstanding, err := untrackedAutomaticDiscoveryForTest(
			t,
			fixture.queue,
			target,
			true,
		); err != nil || outstanding {
			t.Fatalf("other pending URL = %t, %v", outstanding, err)
		}
	})
	t.Run("sequence", func(t *testing.T) {
		fixture := scriptedQueue(t)
		key := string([]byte{1})
		fixture.engine.buckets[automaticOrderIndexBucket][key] = []byte{1}
		fixture.engine.buckets[orderBucket][key] = automaticDiscoveryData(t, target)
		if _, err := untrackedAutomaticDiscoveryForTest(
			t,
			fixture.queue,
			target,
			true,
		); err == nil {
			t.Fatal("malformed pending sequence was accepted")
		}
	})
}

func TestAutomaticDiscoveryOperationalReadFailuresPropagate(t *testing.T) {
	target := "https://read-failure.example/"
	t.Run("active order", func(t *testing.T) {
		fixture := scriptedQueue(t)
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.activeDiscoveryKeys.Put(tx, vault.Key(target), 0)
		}); err != nil {
			t.Fatalf("seed active discovery: %v", err)
		}
		fixture.engine.readErrors[orderBucket] = errors.New("read failed")
		if _, err := automaticDiscoveryOutstandingForTest(
			t,
			fixture.queue,
			target,
			false,
		); err == nil {
			t.Fatal("active order read failure was hidden")
		}
	})
	t.Run("legacy order", func(t *testing.T) {
		fixture := scriptedQueue(t)
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.keys.Put(tx, vault.Key(target), 0)
		}); err != nil {
			t.Fatalf("seed legacy discovery: %v", err)
		}
		fixture.engine.readErrors[orderBucket] = errors.New("read failed")
		if _, err := automaticDiscoveryOutstandingForTest(
			t,
			fixture.queue,
			target,
			false,
		); err == nil {
			t.Fatal("legacy order read failure was hidden")
		}
	})
}

func TestAutomaticDiscoveryOperationalIndexReadFailuresPropagate(t *testing.T) {
	target := "https://read-failure.example/"
	t.Run("pending scan order", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.buckets[automaticOrderIndexBucket][string(orderKey(0))] = []byte{1}
		fixture.engine.readErrors[orderBucket] = errors.New("read failed")
		if _, err := untrackedAutomaticDiscoveryForTest(
			t,
			fixture.queue,
			target,
			true,
		); err == nil {
			t.Fatal("pending scan order read failure was hidden")
		}
	})
	t.Run("active lease index", func(t *testing.T) {
		fixture := scriptedQueue(t)
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.activeDiscoveryKeys.Put(tx, vault.Key(target), 0)
		}); err != nil {
			t.Fatalf("seed active discovery: %v", err)
		}
		fixture.engine.readErrors[leasedDiscoveryKeyBucket] = errors.New("read failed")
		if _, err := automaticDiscoveryOutstandingForTest(
			t,
			fixture.queue,
			target,
			false,
		); err == nil {
			t.Fatal("active lease index read failure was hidden")
		}
	})
	t.Run("legacy lease index", func(t *testing.T) {
		fixture := scriptedQueue(t)
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.keys.Put(tx, vault.Key(target), 0)
		}); err != nil {
			t.Fatalf("seed legacy discovery: %v", err)
		}
		fixture.engine.readErrors[leasedDiscoveryKeyBucket] = errors.New("read failed")
		if _, err := automaticDiscoveryOutstandingForTest(
			t,
			fixture.queue,
			target,
			false,
		); err == nil {
			t.Fatal("legacy lease index read failure was hidden")
		}
	})
}

func TestAutomaticDiscoveryOperationalOwnershipReadFailuresPropagate(t *testing.T) {
	target := "https://read-failure.example/"
	t.Run("pending ownership sidecar", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.readErrors[pendingDiscoveryKeyBucket] = errors.New("read failed")
		err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			_, discardErr := fixture.queue.discardPendingAutomaticDiscoveryLeaseDuplicateTx(
				tx,
				pendingOrderHead{
					index:     fixture.queue.automaticOrderIndex,
					key:       orderKey(0),
					data:      automaticDiscoveryData(t, target),
					found:     true,
					automatic: true,
				},
			)

			return discardErr
		})
		if err == nil {
			t.Fatal("pending ownership sidecar read failure was hidden")
		}
	})
	t.Run("leased ownership sidecar", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.readErrors[leasedDiscoveryKeyBucket] = errors.New("read failed")
		err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.releaseLeasedAutomaticDiscoveryTx(
				tx,
				"lease",
				leaseRecord{DiscoveryKey: target},
			)
		})
		if err == nil {
			t.Fatal("leased ownership sidecar read failure was hidden")
		}
	})
	t.Run("recorded lease sidecar", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.readErrors[leasedDiscoveryKeyBucket] = errors.New("read failed")
		err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.recordLeasedAutomaticDiscoveryTx(
				tx,
				target,
				vault.Key("lease"),
			)
		})
		if err == nil {
			t.Fatal("recorded lease sidecar read failure was hidden")
		}
	})
	t.Run("terminal ownership", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.readErrors[activeDiscoveryKeyBucket] = errors.New("read failed")
		err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.releaseActiveAutomaticDiscoveryTx(
				tx,
				leaseRecord{DiscoveryKey: target},
			)
		})
		if err == nil {
			t.Fatal("terminal ownership read failure was hidden")
		}
	})
}

func TestAutomaticDiscoveryActiveOwnershipRepairFailuresPropagate(t *testing.T) {
	target := "https://active-repair.example/"
	t.Run("pending sidecar", func(t *testing.T) {
		fixture := scriptedQueue(t)
		sequence := seedTrackedAutomaticDiscovery(t, fixture.queue, target)
		delete(fixture.engine.buckets[pendingDiscoveryKeyBucket], string(orderKey(sequence)))
		fixture.engine.putErrors[pendingDiscoveryKeyBucket] = errors.New("write failed")
		if _, err := automaticDiscoveryOutstandingForTest(
			t,
			fixture.queue,
			target,
			true,
		); err == nil {
			t.Fatal("pending sidecar repair failure was hidden")
		}
	})
	t.Run("lease record", func(t *testing.T) {
		fixture := scriptedQueue(t)
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.activeDiscoveryKeys.Put(tx, vault.Key(target), 9)
		}); err != nil {
			t.Fatalf("seed active ownership: %v", err)
		}
		seedAutomaticDiscoveryLease(
			t,
			fixture.queue,
			"lease",
			leaseRecord{OrderData: automaticDiscoveryData(t, target)},
			target,
		)
		fixture.engine.putErrors[leaseBucket] = errors.New("write failed")
		if _, err := automaticDiscoveryOutstandingForTest(
			t,
			fixture.queue,
			target,
			true,
		); err == nil {
			t.Fatal("lease record repair failure was hidden")
		}
	})
	t.Run("lease sidecar", func(t *testing.T) {
		fixture := scriptedQueue(t)
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.activeDiscoveryKeys.Put(tx, vault.Key(target), 9)
		}); err != nil {
			t.Fatalf("seed active ownership: %v", err)
		}
		seedAutomaticDiscoveryLease(
			t,
			fixture.queue,
			"lease",
			leaseRecord{
				OrderData:         automaticDiscoveryData(t, target),
				DiscoveryKey:      target,
				DiscoverySequence: 9,
			},
			"",
		)
		fixture.engine.putErrors[leasedDiscoveryKeyBucket] = errors.New("write failed")
		if _, err := automaticDiscoveryOutstandingForTest(
			t,
			fixture.queue,
			target,
			true,
		); err == nil {
			t.Fatal("lease sidecar repair failure was hidden")
		}
	})
}

func TestAutomaticDiscoveryOrphanOwnershipRepairFailuresPropagate(t *testing.T) {
	target := "https://active-repair.example/"
	for _, bucket := range []vault.Name{
		activeDiscoveryKeyBucket,
		pendingDiscoveryKeyBucket,
		leasedDiscoveryKeyBucket,
	} {
		t.Run("orphan "+string(bucket), func(t *testing.T) {
			fixture := scriptedQueue(t)
			if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
				return fixture.queue.activeDiscoveryKeys.Put(tx, vault.Key(target), 9)
			}); err != nil {
				t.Fatalf("seed orphan ownership: %v", err)
			}
			if bucket == pendingDiscoveryKeyBucket {
				fixture.engine.buckets[bucket][string(orderKey(9))] = []byte(target)
			}
			if bucket == leasedDiscoveryKeyBucket {
				fixture.engine.buckets[bucket][target] = []byte("lease")
			}
			fixture.engine.deleteErrors[bucket] = errors.New("delete failed")
			if _, err := automaticDiscoveryOutstandingForTest(
				t,
				fixture.queue,
				target,
				true,
			); err == nil {
				t.Fatal("orphan ownership repair failure was hidden")
			}
		})
	}
}

func TestAutomaticDiscoveryLegacyPendingMigrationFailuresPropagate(t *testing.T) {
	target := "https://legacy-pending-repair.example/"
	for _, operation := range []struct {
		name   string
		bucket vault.Name
		remove bool
	}{
		{name: "active ownership", bucket: activeDiscoveryKeyBucket},
		{name: "pending sidecar", bucket: pendingDiscoveryKeyBucket},
		{name: "legacy key release", bucket: idempotencyBucket, remove: true},
	} {
		t.Run(operation.name, func(t *testing.T) {
			fixture := scriptedQueue(t)
			order := automaticDiscoveryOrder(target)
			data := automaticDiscoveryData(t, target)
			if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
				sequence, err := fixture.queue.enqueueTx(tx, data, order.Priority)
				if err != nil {
					return err
				}

				return fixture.queue.keys.Put(tx, vault.Key(target), sequence)
			}); err != nil {
				t.Fatalf("seed legacy pending discovery: %v", err)
			}
			if operation.remove {
				fixture.engine.deleteErrors[operation.bucket] = errors.New("delete failed")
			} else {
				fixture.engine.putErrors[operation.bucket] = errors.New("write failed")
			}
			if _, err := automaticDiscoveryOutstandingForTest(
				t,
				fixture.queue,
				target,
				true,
			); err == nil {
				t.Fatal("legacy pending migration failure was hidden")
			}
		})
	}
}

func TestAutomaticDiscoveryLegacyLeaseMigrationFailuresPropagate(t *testing.T) {
	target := "https://legacy-lease-repair.example/"
	for _, operation := range []struct {
		name   string
		bucket vault.Name
	}{
		{name: "lease record", bucket: leaseBucket},
		{name: "lease sidecar", bucket: leasedDiscoveryKeyBucket},
		{name: "active ownership", bucket: activeDiscoveryKeyBucket},
	} {
		t.Run(operation.name, func(t *testing.T) {
			fixture := scriptedQueue(t)
			if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
				return fixture.queue.keys.Put(tx, vault.Key(target), 0)
			}); err != nil {
				t.Fatalf("seed legacy ownership: %v", err)
			}
			seedAutomaticDiscoveryLease(
				t,
				fixture.queue,
				"lease",
				leaseRecord{OrderData: automaticDiscoveryData(t, target)},
				"",
			)
			fixture.engine.putErrors[operation.bucket] = errors.New("write failed")
			if _, err := automaticDiscoveryOutstandingForTest(
				t,
				fixture.queue,
				target,
				true,
			); err == nil {
				t.Fatal("legacy lease migration failure was hidden")
			}
		})
	}
}

func TestAutomaticDiscoveryUntrackedMigrationFailuresPropagate(t *testing.T) {
	target := "https://untracked-repair.example/"
	for _, bucket := range []vault.Name{
		activeDiscoveryKeyBucket,
		pendingDiscoveryKeyBucket,
	} {
		t.Run("pending "+string(bucket), func(t *testing.T) {
			fixture := scriptedQueue(t)
			order := automaticDiscoveryOrder(target)
			if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
				_, err := fixture.queue.enqueueTx(
					tx,
					automaticDiscoveryData(t, target),
					order.Priority,
				)

				return err
			}); err != nil {
				t.Fatalf("seed untracked pending discovery: %v", err)
			}
			fixture.engine.putErrors[bucket] = errors.New("write failed")
			if _, err := untrackedAutomaticDiscoveryForTest(
				t,
				fixture.queue,
				target,
				true,
			); err == nil {
				t.Fatal("untracked pending migration failure was hidden")
			}
		})
	}
	for _, bucket := range []vault.Name{
		leaseBucket,
		leasedDiscoveryKeyBucket,
		activeDiscoveryKeyBucket,
	} {
		t.Run("leased "+string(bucket), func(t *testing.T) {
			fixture := scriptedQueue(t)
			seedAutomaticDiscoveryLease(
				t,
				fixture.queue,
				"lease",
				leaseRecord{OrderData: automaticDiscoveryData(t, target)},
				"",
			)
			fixture.engine.putErrors[bucket] = errors.New("write failed")
			if _, err := untrackedAutomaticDiscoveryForTest(
				t,
				fixture.queue,
				target,
				true,
			); err == nil {
				t.Fatal("untracked lease migration failure was hidden")
			}
		})
	}
}

func TestAutomaticDiscoveryLifecycleBoundaries(t *testing.T) {
	target := "https://lifecycle-boundary.example/"
	t.Run("malformed pending order", func(t *testing.T) {
		fixture := scriptedQueue(t)
		err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			_, _, lookupErr := fixture.queue.activeAutomaticDiscoveryForOrderTx(
				tx,
				[]byte("{"),
				0,
			)

			return lookupErr
		})
		if err == nil {
			t.Fatal("malformed pending order was accepted")
		}
	})
	t.Run("active ownership read", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.readErrors[activeDiscoveryKeyBucket] = errors.New("read failed")
		err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			_, _, lookupErr := fixture.queue.activeAutomaticDiscoveryForOrderTx(
				tx,
				automaticDiscoveryData(t, target),
				0,
			)

			return lookupErr
		})
		if err == nil {
			t.Fatal("active ownership read failure was hidden")
		}
	})
	t.Run("active ownership match", func(t *testing.T) {
		fixture := scriptedQueue(t)
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			if err := fixture.queue.activeDiscoveryKeys.Put(
				tx,
				vault.Key(target),
				7,
			); err != nil {
				return automaticDiscoveryFixtureError(err)
			}
			key, found, err := fixture.queue.activeAutomaticDiscoveryForOrderTx(
				tx,
				automaticDiscoveryData(t, target),
				7,
			)
			if err != nil {
				return err
			}
			if !found || key != target {
				t.Fatalf("active ownership = %q, %t", key, found)
			}

			return nil
		}); err != nil {
			t.Fatalf("match active ownership: %v", err)
		}
	})
}

func TestAutomaticDiscoveryRequeueOwnershipBoundaries(t *testing.T) {
	target := "https://lifecycle-boundary.example/"
	t.Run("requeue leased sidecar release", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.buckets[leasedDiscoveryKeyBucket][target] = []byte("lease")
		fixture.engine.deleteErrors[leasedDiscoveryKeyBucket] = errors.New("delete failed")
		err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.recordRequeuedAutomaticDiscoveryTx(
				tx,
				"lease",
				leaseRecord{DiscoveryKey: target},
				7,
			)
		})
		if err == nil {
			t.Fatal("requeue lease sidecar release failure was hidden")
		}
	})
	for _, bucket := range []vault.Name{
		activeDiscoveryKeyBucket,
		pendingDiscoveryKeyBucket,
	} {
		t.Run("requeue "+string(bucket), func(t *testing.T) {
			fixture := scriptedQueue(t)
			fixture.engine.putErrors[bucket] = errors.New("write failed")
			err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
				return fixture.queue.recordRequeuedAutomaticDiscoveryTx(
					tx,
					"lease",
					leaseRecord{DiscoveryKey: target},
					7,
				)
			})
			if err == nil {
				t.Fatal("requeue ownership failure was hidden")
			}
		})
	}
}

func TestAutomaticDiscoveryTerminalOwnershipBoundaries(t *testing.T) {
	target := "https://lifecycle-boundary.example/"
	t.Run("leased sidecar mismatch", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.buckets[leasedDiscoveryKeyBucket][target] = []byte("other")
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.releaseLeasedAutomaticDiscoveryTx(
				tx,
				"lease",
				leaseRecord{DiscoveryKey: target},
			)
		}); err != nil {
			t.Fatalf("ignore unrelated leased sidecar: %v", err)
		}
	})
	t.Run("leased sidecar delete", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.buckets[leasedDiscoveryKeyBucket][target] = []byte("lease")
		fixture.engine.deleteErrors[leasedDiscoveryKeyBucket] = errors.New("delete failed")
		err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.releaseLeasedAutomaticDiscoveryTx(
				tx,
				"lease",
				leaseRecord{DiscoveryKey: target},
			)
		})
		if err == nil {
			t.Fatal("leased sidecar delete failure was hidden")
		}
	})
	t.Run("empty terminal ownership", func(t *testing.T) {
		fixture := scriptedQueue(t)
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.releaseActiveAutomaticDiscoveryTx(tx, leaseRecord{})
		}); err != nil {
			t.Fatalf("release empty terminal ownership: %v", err)
		}
	})
	t.Run("unrelated terminal ownership", func(t *testing.T) {
		fixture := scriptedQueue(t)
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.releaseActiveAutomaticDiscoveryTx(
				tx,
				leaseRecord{DiscoveryKey: target, DiscoverySequence: 7},
			)
		}); err != nil {
			t.Fatalf("ignore unrelated terminal ownership: %v", err)
		}
	})
	t.Run("terminal ownership delete", func(t *testing.T) {
		fixture := scriptedQueue(t)
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.activeDiscoveryKeys.Put(tx, vault.Key(target), 7)
		}); err != nil {
			t.Fatalf("seed terminal ownership: %v", err)
		}
		fixture.engine.deleteErrors[activeDiscoveryKeyBucket] = errors.New("delete failed")
		err := fixture.queue.releaseActiveAutomaticDiscovery(
			t.Context(),
			leaseRecord{DiscoveryKey: target, DiscoverySequence: 7},
		)
		if err == nil {
			t.Fatal("terminal ownership delete failure was hidden")
		}
	})
	t.Run("terminal ownership transaction", func(t *testing.T) {
		fixture := scriptedQueue(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := fixture.queue.releaseActiveAutomaticDiscovery(
			ctx,
			leaseRecord{DiscoveryKey: target},
		); !errors.Is(err, context.Canceled) {
			t.Fatalf("terminal ownership transaction error = %v", err)
		}
	})
}

func TestAutomaticDiscoveryDuplicateDiscardBoundaries(t *testing.T) {
	target := "https://duplicate-discard.example/"
	t.Run("sequence", func(t *testing.T) {
		fixture := scriptedQueue(t)
		err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			_, discardErr := fixture.queue.discardPendingAutomaticDiscoveryLeaseDuplicateTx(
				tx,
				pendingOrderHead{key: []byte{1}, automatic: true},
			)

			return discardErr
		})
		if err == nil {
			t.Fatal("malformed duplicate sequence was accepted")
		}
	})
	t.Run("missing sidecar read", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.readErrors[activeDiscoveryKeyBucket] = errors.New("read failed")
		err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			_, discardErr := fixture.queue.discardPendingAutomaticDiscoveryLeaseDuplicateTx(
				tx,
				pendingOrderHead{
					index:     fixture.queue.automaticOrderIndex,
					key:       orderKey(7),
					data:      automaticDiscoveryData(t, target),
					found:     true,
					automatic: true,
				},
			)

			return discardErr
		})
		if err == nil {
			t.Fatal("missing sidecar ownership read failure was hidden")
		}
	})
	t.Run("missing sidecar ownership", func(t *testing.T) {
		fixture := scriptedQueue(t)
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			if err := fixture.queue.activeDiscoveryKeys.Put(
				tx,
				vault.Key(target),
				7,
			); err != nil {
				return automaticDiscoveryFixtureError(err)
			}
			discarded, err := fixture.queue.discardPendingAutomaticDiscoveryLeaseDuplicateTx(
				tx,
				pendingOrderHead{
					index:     fixture.queue.automaticOrderIndex,
					key:       orderKey(7),
					data:      automaticDiscoveryData(t, target),
					found:     true,
					automatic: true,
				},
			)
			if err != nil {
				return err
			}
			if discarded {
				t.Fatal("unleased pending ownership was discarded")
			}

			return nil
		}); err != nil {
			t.Fatalf("inspect missing sidecar ownership: %v", err)
		}
	})
}

func TestAutomaticDiscoveryDuplicateDiscardFailureOperations(t *testing.T) {
	target := "https://duplicate-discard.example/"
	for _, operation := range []struct {
		name    string
		bucket  vault.Name
		write   bool
		unindex bool
	}{
		{name: "order delete", bucket: orderBucket},
		{name: "priority delete", bucket: automaticOrderIndexBucket},
		{name: "sidecar delete", bucket: pendingDiscoveryKeyBucket},
		{name: "lease repair", bucket: leaseBucket, write: true},
		{
			name:    "lease sidecar repair",
			bucket:  leasedDiscoveryKeyBucket,
			write:   true,
			unindex: true,
		},
		{name: "active repair", bucket: activeDiscoveryKeyBucket, write: true},
	} {
		t.Run(operation.name, func(t *testing.T) {
			fixture := scriptedQueue(t)
			order := duplicatePendingAutomaticDiscovery(t, fixture, target)
			if operation.unindex {
				delete(fixture.engine.buckets[leasedDiscoveryKeyBucket], target)
			}
			if operation.write {
				fixture.engine.putErrors[operation.bucket] = errors.New("write failed")
			} else {
				fixture.engine.deleteErrors[operation.bucket] = errors.New("delete failed")
			}
			err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
				_, discardErr := fixture.queue.discardPendingAutomaticDiscoveryLeaseDuplicateTx(
					tx,
					order,
				)

				return discardErr
			})
			if err == nil {
				t.Fatal("duplicate discard failure was hidden")
			}
		})
	}
}

func TestAutomaticDiscoveryIntentReconciliationFailuresPropagate(t *testing.T) {
	target := "https://reconciliation-failure.example/"
	t.Run("intent scan", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.scanErrors[discoveryIntentBucket] = errors.New("scan failed")
		if err := fixture.queue.reconcileAutomaticDiscoveryIntents(
			t.Context(),
		); err == nil {
			t.Fatal("intent scan failure was hidden")
		}
	})
	t.Run("recovered intent release", func(t *testing.T) {
		fixture := scriptedQueue(t)
		if err := fixture.queue.persistAutomaticDiscoveryIntent(
			t.Context(),
			target,
			automaticDiscoveryData(t, target),
		); err != nil {
			t.Fatalf("persist recovery intent: %v", err)
		}
		fixture.engine.deleteErrors[discoveryIntentBucket] = errors.New("delete failed")
		if err := fixture.queue.reconcileAutomaticDiscoveryIntents(
			t.Context(),
		); err == nil {
			t.Fatal("recovered intent release failure was hidden")
		}
	})
}

func TestAutomaticDiscoveryLeaseReconciliationFailuresPropagate(t *testing.T) {
	target := "https://reconciliation-failure.example/"
	t.Run("lease sidecar repair", func(t *testing.T) {
		fixture := scriptedQueue(t)
		seedAutomaticDiscoveryLease(
			t,
			fixture.queue,
			"lease",
			leaseRecord{
				OrderData:         automaticDiscoveryData(t, target),
				DiscoveryKey:      target,
				DiscoverySequence: 7,
			},
			"",
		)
		fixture.engine.putErrors[leasedDiscoveryKeyBucket] = errors.New("write failed")
		if err := fixture.queue.reconcileAutomaticDiscoveryLeases(
			t.Context(),
		); err == nil {
			t.Fatal("lease sidecar repair failure was hidden")
		}
	})
	t.Run("active ownership read", func(t *testing.T) {
		fixture := scriptedQueue(t)
		seedAutomaticDiscoveryLease(
			t,
			fixture.queue,
			"lease",
			leaseRecord{
				OrderData:         automaticDiscoveryData(t, target),
				DiscoveryKey:      target,
				DiscoverySequence: 7,
			},
			target,
		)
		fixture.engine.readErrors[activeDiscoveryKeyBucket] = errors.New("read failed")
		if err := fixture.queue.reconcileAutomaticDiscoveryLeases(
			t.Context(),
		); err == nil {
			t.Fatal("active ownership read failure was hidden")
		}
	})
	t.Run("active ownership repair", func(t *testing.T) {
		fixture := scriptedQueue(t)
		seedAutomaticDiscoveryLease(
			t,
			fixture.queue,
			"lease",
			leaseRecord{
				OrderData:         automaticDiscoveryData(t, target),
				DiscoveryKey:      target,
				DiscoverySequence: 7,
			},
			target,
		)
		fixture.engine.putErrors[activeDiscoveryKeyBucket] = errors.New("write failed")
		if err := fixture.queue.reconcileAutomaticDiscoveryLeases(
			t.Context(),
		); err == nil {
			t.Fatal("active ownership repair failure was hidden")
		}
	})
}

func TestAutomaticDiscoveryIntentCompletionFailuresPropagate(t *testing.T) {
	target := "https://reconciliation-failure.example/"
	t.Run("intent decode", func(t *testing.T) {
		fixture := scriptedQueue(t)
		if _, err := fixture.queue.completeAutomaticDiscoveryIntent(
			t.Context(),
			target,
			[]byte("{"),
			false,
		); err == nil {
			t.Fatal("malformed discovery intent was accepted")
		}
	})
	for _, bucket := range []vault.Name{
		activeDiscoveryKeyBucket,
		pendingDiscoveryKeyBucket,
	} {
		t.Run("intent completion "+string(bucket), func(t *testing.T) {
			fixture := scriptedQueue(t)
			fixture.engine.putErrors[bucket] = errors.New("write failed")
			if _, err := fixture.queue.completeAutomaticDiscoveryIntent(
				t.Context(),
				target,
				automaticDiscoveryData(t, target),
				false,
			); err == nil {
				t.Fatal("intent completion failure was hidden")
			}
		})
	}
}

type automaticDiscoveryLeaseCatalogFailureEngine struct {
	*scriptedEngine
	views int
}

func (e *automaticDiscoveryLeaseCatalogFailureEngine) View(
	ctx context.Context,
	fn func(vault.EngineTxn) error,
) error {
	e.views++
	if e.views == 2 {
		e.scanErrors[leaseBucket] = errors.New("scan failed")
	}

	return e.scriptedEngine.View(ctx, fn)
}

func TestAutomaticDiscoveryQueueStartupFailuresPropagate(t *testing.T) {
	t.Run("recovery intent", func(t *testing.T) {
		engine := newScriptedEngine()
		storage, err := vault.New(engine)
		if err != nil {
			t.Fatalf("open storage: %v", err)
		}
		engine.scanErrors[discoveryIntentBucket] = errors.New("scan failed")
		if _, err := newDurableOrderQueue(
			storage,
			DefaultLeaseTTL,
		); err == nil {
			t.Fatal("startup recovery intent failure was hidden")
		}
	})
	t.Run("worker lease catalog", func(t *testing.T) {
		engine := &automaticDiscoveryLeaseCatalogFailureEngine{
			scriptedEngine: newScriptedEngine(),
		}
		storage, err := vault.New(engine)
		if err != nil {
			t.Fatalf("open storage: %v", err)
		}
		if _, err := newDurableOrderQueue(
			storage,
			DefaultLeaseTTL,
		); err == nil {
			t.Fatal("startup worker lease catalog failure was hidden")
		}
	})
	for _, bucket := range []vault.Name{
		activeDiscoveryKeyBucket,
		pendingDiscoveryKeyBucket,
		leasedDiscoveryKeyBucket,
		discoveryIntentBucket,
	} {
		t.Run("register "+string(bucket), func(t *testing.T) {
			engine := newScriptedEngine()
			engine.provisionErrors[bucket] = errors.New("provision failed")
			storage, err := vault.New(engine)
			if err != nil {
				t.Fatalf("open storage: %v", err)
			}
			if _, err := newDurableOrderQueue(
				storage,
				DefaultLeaseTTL,
			); err == nil {
				t.Fatal("automatic discovery collection failure was hidden")
			}
		})
	}
}
