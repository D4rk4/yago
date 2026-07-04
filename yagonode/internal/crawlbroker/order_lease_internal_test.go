package crawlbroker

import (
	"context"
	"errors"
	"fmt"
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
	if _, ok := leaseRecordFor(t, queue, leaseID); ok {
		t.Fatal("lease was not deleted by ack")
	}
	if n := pendingCount(t, queue); n != 0 {
		t.Fatalf("pending = %d, want 0 after ack", n)
	}
}

func TestAckLeaseUnknownIsNoop(t *testing.T) {
	if err := memQueue(t).ackLease(context.Background(), "missing"); err != nil {
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

func TestRequeueLeaseReturnsOrderToPending(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "again", "w1")
	if err := queue.requeueLease(context.Background(), leaseID); err != nil {
		t.Fatalf("requeue: %v", err)
	}
	if _, ok := leaseRecordFor(t, queue, leaseID); ok {
		t.Fatal("lease still present after requeue")
	}
	if n := pendingCount(t, queue); n != 1 {
		t.Fatalf("pending = %d, want 1 after requeue", n)
	}
}

func TestRequeueLeaseUnknownIsNoop(t *testing.T) {
	queue := memQueue(t)
	if err := queue.requeueLease(context.Background(), "missing"); err != nil {
		t.Fatalf("requeue unknown lease: %v", err)
	}
	if n := pendingCount(t, queue); n != 0 {
		t.Fatalf("pending = %d, want 0", n)
	}
}

func TestRequeueLeaseSurfacesErrors(t *testing.T) {
	getFail := scriptedQueue(t)
	leaseID := leaseOne(t, getFail.queue, "g", "w1")
	getFail.engine.buckets[leaseBucket][leaseID] = []byte("not json")
	if err := getFail.queue.requeueLease(context.Background(), leaseID); err == nil {
		t.Fatal("expected requeue to surface a lease decode error")
	}

	deleteFail := scriptedQueue(t)
	deleteLease := leaseOne(t, deleteFail.queue, "d", "w1")
	deleteFail.engine.deleteErrors[leaseBucket] = errors.New("delete failed")
	if err := deleteFail.queue.requeueLease(context.Background(), deleteLease); err == nil {
		t.Fatal("expected requeue to surface a delete error")
	}

	enqueueFail := scriptedQueue(t)
	enqueueLease := leaseOne(t, enqueueFail.queue, "e", "w1")
	enqueueFail.engine.putErrors[seqBucket] = errors.New("seq put failed")
	if err := enqueueFail.queue.requeueLease(context.Background(), enqueueLease); err == nil {
		t.Fatal("expected requeue to surface an enqueue error")
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

func TestRequeueAllLeasesReturnsEverything(t *testing.T) {
	queue := memQueue(t)
	_ = leaseOne(t, queue, "a", "w1")
	_ = leaseOne(t, queue, "b", "w2")
	if err := queue.requeueAllLeases(context.Background()); err != nil {
		t.Fatalf("requeue all: %v", err)
	}
	if n := pendingCount(t, queue); n != 2 {
		t.Fatalf("pending = %d, want 2 after reclaim", n)
	}
}

func TestRequeueLeasesMatchingSurfacesErrors(t *testing.T) {
	scanFail := scriptedQueue(t)
	scanFail.engine.scanErrors[leaseBucket] = errors.New("scan failed")
	if err := scanFail.queue.requeueAllLeases(context.Background()); err == nil {
		t.Fatal("expected reclaim to surface a scan error")
	}

	deleteFail := scriptedQueue(t)
	_ = leaseOne(t, deleteFail.queue, "d", "w1")
	deleteFail.engine.deleteErrors[leaseBucket] = errors.New("delete failed")
	if err := deleteFail.queue.requeueAllLeases(context.Background()); err == nil {
		t.Fatal("expected reclaim to surface a delete error")
	}

	enqueueFail := scriptedQueue(t)
	_ = leaseOne(t, enqueueFail.queue, "e", "w1")
	enqueueFail.engine.putErrors[orderBucket] = errors.New("orders put failed")
	if err := enqueueFail.queue.requeueAllLeases(context.Background()); err == nil {
		t.Fatal("expected reclaim to surface an enqueue error")
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

func TestLeaseRecordCodecRejectsBadJSON(t *testing.T) {
	if _, err := (leaseRecordCodec{}).Decode([]byte("not json")); err == nil {
		t.Fatal("expected lease record decode error")
	}
}
