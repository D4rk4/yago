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

func withClock(t *testing.T) func(time.Time) {
	t.Helper()
	var current time.Time
	restore := nowFunc
	t.Cleanup(func() { nowFunc = restore })
	nowFunc = func() time.Time { return current }

	return func(at time.Time) { current = at }
}

func withLeaseIDError(t *testing.T) {
	t.Helper()
	restore := newLeaseID
	t.Cleanup(func() { newLeaseID = restore })
	newLeaseID = func() (string, error) { return "", errors.New("mint failed") }
}

func leaseOne(t *testing.T, q *DurableOrderQueue, name, worker string) string {
	t.Helper()
	if err := q.Publish(context.Background(), testOrder(name)); err != nil {
		t.Fatalf("publish %s: %v", name, err)
	}
	_, leaseID, ok, err := q.leasePop(context.Background(), worker)
	if err != nil || !ok {
		t.Fatalf("lease %s: ok=%v err=%v", name, ok, err)
	}

	return leaseID
}

func pendingCount(t *testing.T, q *DurableOrderQueue) int {
	t.Helper()
	count := 0
	if err := q.vault.View(context.Background(), func(tx *vault.Txn) error {
		return q.orders.Scan(tx, nil, func(vault.Key, []byte) (bool, error) {
			count++

			return true, nil
		})
	}); err != nil {
		t.Fatalf("count pending: %v", err)
	}

	return count
}

func leaseRecordFor(t *testing.T, q *DurableOrderQueue, leaseID string) (leaseRecord, bool) {
	t.Helper()
	var record leaseRecord
	found := false
	if err := q.vault.View(context.Background(), func(tx *vault.Txn) error {
		got, ok, err := q.leases.Get(tx, vault.Key(leaseID))
		if err != nil {
			return fmt.Errorf("get lease: %w", err)
		}
		record, found = got, ok

		return nil
	}); err != nil {
		t.Fatalf("read lease: %v", err)
	}

	return record, found
}

func TestAckLeaseDeletesLease(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "done", "w1")
	if err := queue.ackLease(context.Background(), leaseID); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if err := queue.ackLease(context.Background(), leaseID); err != nil {
		t.Fatalf("duplicate ack: %v", err)
	}
	if _, ok := leaseRecordFor(t, queue, leaseID); ok {
		t.Fatal("lease was not deleted by ack")
	}
	if n := pendingCount(t, queue); n != 0 {
		t.Fatalf("pending = %d, want 0 after ack", n)
	}
}

func TestAckLeaseUnknownIsRejected(t *testing.T) {
	if err := memQueue(
		t,
	).ackLease(context.Background(), "missing"); !errors.Is(
		err,
		errLeaseDispositionConflict,
	) {
		t.Fatalf("ack unknown lease: %v", err)
	}
}

func TestAckLeaseSurfacesDeleteError(t *testing.T) {
	fixture := scriptedQueue(t)
	leaseID := leaseOne(t, fixture.queue, "x", "w1")
	fixture.engine.deleteErrors[leaseBucket] = errors.New("delete failed")
	if err := fixture.queue.ackLease(context.Background(), leaseID); err == nil {
		t.Fatal("expected ack to surface a delete error")
	}
}

func TestAckLeaseSurfacesReadAndSettlementErrors(t *testing.T) {
	readFail := scriptedQueue(t)
	readLeaseID := leaseOne(t, readFail.queue, "read", "w1")
	readFail.engine.buckets[leaseBucket][readLeaseID] = []byte("not json")
	if err := readFail.queue.ackLease(t.Context(), readLeaseID); err == nil {
		t.Fatal("expected ack to surface a lease read error")
	}

	settlementFail := scriptedQueue(t)
	settlementLeaseID := leaseOne(t, settlementFail.queue, "settlement", "w1")
	settlementFail.engine.putErrors[leaseSettlementBucket] = errors.New("put failed")
	if err := settlementFail.queue.ackLease(t.Context(), settlementLeaseID); err == nil {
		t.Fatal("expected ack to surface a settlement error")
	}
}

func TestDeferredLeaseReturnsOrderToPendingAfterRetryDeadline(t *testing.T) {
	set := withClock(t)
	base := time.Unix(900, 0)
	set(base)
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "again", "w1")
	if err := queue.deferLease(context.Background(), leaseID); err != nil {
		t.Fatalf("defer: %v", err)
	}
	record, ok := leaseRecordFor(t, queue, leaseID)
	if !ok || record.WorkerID != "" || !record.Deferred {
		t.Fatalf("deferred lease = %#v/%v, want retained without an owner", record, ok)
	}
	if record.ExpiresAtUnixNano != base.Add(negativeAcknowledgmentRetryDelay).UnixNano() {
		t.Fatalf("retry deadline = %d, want fixed delay", record.ExpiresAtUnixNano)
	}
	if n := pendingCount(t, queue); n != 0 {
		t.Fatalf("pending = %d, want 0 before retry deadline", n)
	}
	set(base.Add(negativeAcknowledgmentRetryDelay))
	if err := queue.sweepExpired(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if _, ok := leaseRecordFor(t, queue, leaseID); ok {
		t.Fatal("deferred lease remained after retry deadline")
	}
	if n := pendingCount(t, queue); n != 1 {
		t.Fatalf("pending = %d, want 1 after retry deadline", n)
	}
}

func TestDeferLeaseUnknownIsRejected(t *testing.T) {
	queue := memQueue(t)
	if err := queue.deferLease(
		context.Background(),
		"missing",
	); !errors.Is(
		err,
		errLeaseDispositionConflict,
	) {
		t.Fatalf("defer unknown lease: %v", err)
	}
	if n := pendingCount(t, queue); n != 0 {
		t.Fatalf("pending = %d, want 0", n)
	}
}

func TestDeferLeaseSurfacesErrors(t *testing.T) {
	getFail := scriptedQueue(t)
	leaseID := leaseOne(t, getFail.queue, "g", "w1")
	getFail.engine.buckets[leaseBucket][leaseID] = []byte("not json")
	if err := getFail.queue.deferLease(context.Background(), leaseID); err == nil {
		t.Fatal("expected defer to surface a lease decode error")
	}

	putFail := scriptedQueue(t)
	putLease := leaseOne(t, putFail.queue, "p", "w1")
	putFail.engine.putErrors[leaseBucket] = errors.New("put failed")
	if err := putFail.queue.deferLease(context.Background(), putLease); err == nil {
		t.Fatal("expected defer to surface a lease update error")
	}

	settlementFail := scriptedQueue(t)
	settlementLease := leaseOne(t, settlementFail.queue, "s", "w1")
	settlementFail.engine.putErrors[leaseSettlementBucket] = errors.New("put failed")
	if err := settlementFail.queue.deferLease(context.Background(), settlementLease); err == nil {
		t.Fatal("expected defer to surface a settlement error")
	}
}

func TestHeartbeatExtendsOnlyMatchingWorker(t *testing.T) {
	set := withClock(t)
	base := time.Unix(1000, 0)
	set(base)
	queue := memQueue(t)
	queue.leaseTTL = time.Minute
	mineLease := leaseOne(t, queue, "mine", "w1")
	otherLease := leaseOne(t, queue, "other", "w2")

	set(base.Add(30 * time.Second))
	if err := queue.heartbeat(context.Background(), "w1"); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	mine, _ := leaseRecordFor(t, queue, mineLease)
	if mine.ExpiresAtUnixNano != base.Add(30*time.Second).Add(time.Minute).UnixNano() {
		t.Fatalf("w1 lease deadline not extended: %d", mine.ExpiresAtUnixNano)
	}
	other, _ := leaseRecordFor(t, queue, otherLease)
	if other.ExpiresAtUnixNano != base.Add(time.Minute).UnixNano() {
		t.Fatalf("w2 lease deadline changed: %d", other.ExpiresAtUnixNano)
	}
}

func TestHeartbeatSurfacesErrors(t *testing.T) {
	scanFail := scriptedQueue(t)
	scanFail.engine.scanErrors[leaseBucket] = errors.New("scan failed")
	if err := scanFail.queue.heartbeat(context.Background(), "w1"); err == nil {
		t.Fatal("expected heartbeat to surface a scan error")
	}

	putFail := scriptedQueue(t)
	_ = leaseOne(t, putFail.queue, "p", "w1")
	putFail.engine.putErrors[leaseBucket] = errors.New("put failed")
	if err := putFail.queue.heartbeat(context.Background(), "w1"); err == nil {
		t.Fatal("expected heartbeat to surface a put error")
	}
}

func TestSweepExpiredRequeuesOnlyExpired(t *testing.T) {
	set := withClock(t)
	base := time.Unix(2000, 0)
	set(base)
	queue := memQueue(t)
	queue.leaseTTL = time.Minute
	_ = leaseOne(t, queue, "stale", "w1")

	set(base.Add(30 * time.Second))
	if err := queue.sweepExpired(context.Background()); err != nil {
		t.Fatalf("sweep before expiry: %v", err)
	}
	if n := pendingCount(t, queue); n != 0 {
		t.Fatalf("pending = %d, want 0 before expiry", n)
	}

	set(base.Add(2 * time.Minute))
	if err := queue.sweepExpired(context.Background()); err != nil {
		t.Fatalf("sweep after expiry: %v", err)
	}
	if n := pendingCount(t, queue); n != 1 {
		t.Fatalf("pending = %d, want 1 after expiry", n)
	}
}

func TestSweepExpiredPurgesStaleHeartbeatGates(t *testing.T) {
	set := withClock(t)
	base := time.Unix(2500, 0)
	set(base)
	queue := memQueue(t)
	queue.leaseTTL = time.Minute
	queue.extendedAt["stopped-worker"] = base
	set(base.Add(time.Minute))
	if err := queue.sweepExpired(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if _, retained := queue.extendedAt["stopped-worker"]; retained {
		t.Fatal("stale heartbeat gate remained after sweep")
	}
}

func TestHeartbeatWithoutLeasesRemovesWorkerGate(t *testing.T) {
	set := withClock(t)
	base := time.Unix(2600, 0)
	set(base)
	queue := memQueue(t)
	queue.leaseTTL = time.Minute
	queue.extendedAt["worker"] = base
	set(base.Add(time.Minute))
	if err := queue.heartbeat(context.Background(), "worker"); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if _, retained := queue.extendedAt["worker"]; retained {
		t.Fatal("worker without leases retained a heartbeat gate")
	}
}

func TestRequeueLeasesMatchingSurfacesErrors(t *testing.T) {
	scanFail := scriptedQueue(t)
	scanFail.engine.scanErrors[leaseBucket] = errors.New("scan failed")
	if err := scanFail.queue.sweepExpired(context.Background()); err == nil {
		t.Fatal("expected reclaim to surface a scan error")
	}

	set := withClock(t)
	set(time.Unix(1000, 0))
	deleteFail := scriptedQueue(t)
	_ = leaseOne(t, deleteFail.queue, "d", "w1")
	set(time.Unix(2000, 0))
	deleteFail.engine.deleteErrors[leaseBucket] = errors.New("delete failed")
	if err := deleteFail.queue.sweepExpired(context.Background()); err == nil {
		t.Fatal("expected reclaim to surface a delete error")
	}

	set(time.Unix(1000, 0))
	enqueueFail := scriptedQueue(t)
	_ = leaseOne(t, enqueueFail.queue, "e", "w1")
	set(time.Unix(2000, 0))
	enqueueFail.engine.putErrors[orderBucket] = errors.New("orders put failed")
	if err := enqueueFail.queue.sweepExpired(context.Background()); err == nil {
		t.Fatal("expected reclaim to surface an enqueue error")
	}

	set(time.Unix(1000, 0))
	settlementFail := scriptedQueue(t)
	_ = leaseOne(t, settlementFail.queue, "s", "w1")
	set(time.Unix(2000, 0))
	settlementFail.engine.putErrors[leaseSettlementBucket] = errors.New("settlement put failed")
	if err := settlementFail.queue.sweepExpired(context.Background()); err == nil {
		t.Fatal("expected reclaim to surface a settlement error")
	}
}

func TestSweepExpiredMovesBoundedChunksAndAllowsLeaseRenewal(t *testing.T) {
	set := withClock(t)
	base := time.Unix(2_700, 0)
	set(base)
	queue := memQueue(t)
	queue.leaseTTL = time.Minute
	expired := maximumLeaseRequeueChunk + 44
	for index := range expired {
		leaseOne(
			t,
			queue,
			fmt.Sprintf("expired-%d", index),
			"expired-worker",
		)
	}
	set(base.Add(time.Minute))
	liveLeaseID := leaseOneForSession(
		t,
		queue,
		"live",
		"live-worker",
		"live-session",
	)
	firstChunk := make(chan struct{})
	continueSweep := make(chan struct{})
	var once sync.Once
	previousHook := afterLeaseRequeueChunk
	t.Cleanup(func() { afterLeaseRequeueChunk = previousHook })
	afterLeaseRequeueChunk = func() {
		once.Do(func() {
			close(firstChunk)
			<-continueSweep
		})
	}
	sweepDone := make(chan error, 1)
	go func() { sweepDone <- queue.sweepExpired(t.Context()) }()
	select {
	case <-firstChunk:
	case <-time.After(time.Second):
		close(continueSweep)
		t.Fatal("sweep did not complete its first chunk")
	}
	renewDone := make(chan error, 1)
	go func() {
		renewed, _, err := queue.renewLeases(
			t.Context(),
			"live-worker",
			"live-session",
			[]string{liveLeaseID},
		)
		if err == nil && (len(renewed) != 1 || renewed[0] != liveLeaseID) {
			err = fmt.Errorf("renewed leases = %v", renewed)
		}
		renewDone <- err
	}()
	select {
	case err := <-renewDone:
		if err != nil {
			close(continueSweep)
			t.Fatalf("interleaved renewal: %v", err)
		}
	case <-time.After(time.Second):
		close(continueSweep)
		t.Fatal("lease renewal could not interleave between sweep chunks")
	}
	close(continueSweep)
	select {
	case err := <-sweepDone:
		if err != nil {
			t.Fatalf("sweep expired chunks: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("chunked sweep did not finish")
	}
	if pendingCount(t, queue) != expired {
		t.Fatalf("pending expired orders = %d, want %d", pendingCount(t, queue), expired)
	}
	if _, found := leaseRecordFor(t, queue, liveLeaseID); !found {
		t.Fatal("interleaved live lease was swept")
	}
}

func TestLeasePopSurfacesLeaseIDError(t *testing.T) {
	withLeaseIDError(t)
	queue := memQueue(t)
	if err := queue.Publish(context.Background(), testOrder("x")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, _, _, err := queue.leasePop(context.Background(), "w1"); err == nil {
		t.Fatal("expected lease pop to surface a lease id error")
	}
}

func TestLeasePopSurfacesLeasePutError(t *testing.T) {
	fixture := scriptedQueue(t)
	if err := fixture.queue.Publish(context.Background(), testOrder("x")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	fixture.engine.putErrors[leaseBucket] = errors.New("lease put failed")
	if _, _, _, err := fixture.queue.leasePop(context.Background(), "w1"); err == nil {
		t.Fatal("expected lease pop to surface a lease put error")
	}
}

func TestLeasePopReplayDropsAbortedOrderState(t *testing.T) {
	fixture := scriptedQueue(t)
	if err := fixture.queue.Publish(context.Background(), testOrder("stale")); err != nil {
		t.Fatal(err)
	}
	fixture.engine.replayNext = true
	fixture.engine.betweenReplay = func() {
		clear(fixture.engine.buckets[orderBucket])
	}
	data, _, found, err := fixture.queue.leasePop(context.Background(), "worker")
	if err != nil {
		t.Fatal(err)
	}
	if found || data != nil {
		t.Fatalf("lease = %q/%v, want empty replay result", data, found)
	}
	if len(fixture.engine.buckets[leaseBucket]) != 0 {
		t.Fatalf("leases = %v, want no stale lease", fixture.engine.buckets[leaseBucket])
	}
}

func TestLeaseRecordCodecRejectsBadJSON(t *testing.T) {
	if _, err := (leaseRecordCodec{}).Decode([]byte("not json")); err == nil {
		t.Fatal("expected lease record decode error")
	}
}

// TestHeartbeatSkipsDurableExtensionWithinWindow is the IO-AGG-02 acceptance:
// heartbeats inside leaseTTL/4 of the last durable extension skip the write
// entirely, and the first beat past the window extends again — so a worker
// beating every few seconds costs one fsync per quarter-TTL, not one per beat.
func TestHeartbeatSkipsDurableExtensionWithinWindow(t *testing.T) {
	set := withClock(t)
	base := time.Unix(1000, 0)
	set(base)
	queue := memQueue(t)
	queue.leaseTTL = time.Minute
	lease := leaseOne(t, queue, "mine", "w1")

	if err := queue.heartbeat(context.Background(), "w1"); err != nil {
		t.Fatalf("first heartbeat: %v", err)
	}
	first, _ := leaseRecordFor(t, queue, lease)
	if first.ExpiresAtUnixNano != base.Add(time.Minute).UnixNano() {
		t.Fatalf("first heartbeat did not extend: %d", first.ExpiresAtUnixNano)
	}

	set(base.Add(5 * time.Second))
	if err := queue.heartbeat(context.Background(), "w1"); err != nil {
		t.Fatalf("gated heartbeat: %v", err)
	}
	gated, _ := leaseRecordFor(t, queue, lease)
	if gated.ExpiresAtUnixNano != first.ExpiresAtUnixNano {
		t.Fatalf("heartbeat within the window rewrote the lease: %d", gated.ExpiresAtUnixNano)
	}

	set(base.Add(20 * time.Second))
	if err := queue.heartbeat(context.Background(), "w1"); err != nil {
		t.Fatalf("post-window heartbeat: %v", err)
	}
	extended, _ := leaseRecordFor(t, queue, lease)
	want := base.Add(20 * time.Second).Add(time.Minute).UnixNano()
	if extended.ExpiresAtUnixNano != want {
		t.Fatalf("post-window heartbeat deadline = %d, want %d", extended.ExpiresAtUnixNano, want)
	}
}
