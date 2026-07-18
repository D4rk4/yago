package crawlbroker

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestExpiredCheckpointLeasesRemainSeparatedByWorker(t *testing.T) {
	set := withClock(t)
	base := time.Unix(80_000, 0)
	set(base)
	queue := memQueue(t)
	queue.leaseTTL = time.Minute
	leaseA := leaseOneForSession(t, queue, "worker-a", "worker-a", "session-a")
	leaseB := leaseOneForSession(t, queue, "worker-b", "worker-b", "session-b")
	set(base.Add(2 * time.Minute))
	if err := queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("sweep checkpoint leases: %v", err)
	}
	if pendingCount(t, queue) != 0 {
		t.Fatalf("pending checkpoint leases = %d", pendingCount(t, queue))
	}
	if _, _, found, err := queue.leasePopForSession(
		t.Context(),
		"worker-c",
		"session-c",
	); err != nil || found {
		t.Fatalf("unrelated worker claim found=%v err=%v", found, err)
	}
	adoptedA, err := queue.adoptWorkerSession(t.Context(), "worker-a", "session-a2")
	if err != nil {
		t.Fatalf("adopt worker-a: %v", err)
	}
	if len(adoptedA) != 1 || adoptedA[0].LeaseID != leaseA {
		t.Fatalf("worker-a adoption = %+v, want %s", adoptedA, leaseA)
	}
	recordA, found := leaseRecordFor(t, queue, leaseA)
	if !found || recordA.WorkerSessionID != "session-a2" ||
		recordA.ExpiresAtUnixNano != base.Add(3*time.Minute).UnixNano() {
		t.Fatalf("worker-a lease = %+v/%v", recordA, found)
	}
	recordB, found := leaseRecordFor(t, queue, leaseB)
	if !found || recordB.WorkerSessionID != "session-b" ||
		recordB.ExpiresAtUnixNano != base.Add(time.Minute).UnixNano() {
		t.Fatalf("worker-b parked lease = %+v/%v", recordB, found)
	}
	adoptedB, err := queue.adoptWorkerSession(t.Context(), "worker-b", "session-b2")
	if err != nil || len(adoptedB) != 1 || adoptedB[0].LeaseID != leaseB {
		t.Fatalf("worker-b adoption = %+v, err=%v", adoptedB, err)
	}
}

func TestCheckpointLeaseAndControlSurviveBrokerRestarts(t *testing.T) {
	set := withClock(t)
	base := time.Unix(90_000, 0)
	set(base)
	path := filepath.Join(t.TempDir(), "node.db")
	storage := openCheckpointLeaseStorage(t, path)
	queue := openCheckpointLeaseQueue(t, storage)
	leaseID := leaseOneForSession(
		t,
		queue,
		"restart",
		"stable-worker",
		"process-session-a",
	)
	if err := storage.Close(); err != nil {
		t.Fatalf("close first storage: %v", err)
	}

	set(base.Add(2 * time.Minute))
	storage = openCheckpointLeaseStorage(t, path)
	queue = openCheckpointLeaseQueue(t, storage)
	if err := queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("sweep after first restart: %v", err)
	}
	if _, found := leaseRecordFor(t, queue, leaseID); !found {
		t.Fatal("checkpoint lease was lost after broker restart")
	}
	if _, _, found, err := queue.leasePopForSession(
		t.Context(),
		"other-worker",
		"other-session",
	); err != nil || found {
		t.Fatalf("other worker claim found=%v err=%v", found, err)
	}
	control, err := newPersistentControlRegistry(storage)
	if err != nil {
		t.Fatalf("open persistent controls: %v", err)
	}
	if !control.Enqueue("stable-worker", yagocrawlcontract.CrawlControlDirective{
		Kind:  yagocrawlcontract.CrawlControlCancel,
		RunID: testOrderRunID,
	}) {
		t.Fatal("parked run rejected cancellation")
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close second storage: %v", err)
	}

	set(base.Add(4 * time.Minute))
	storage = openCheckpointLeaseStorage(t, path)
	t.Cleanup(func() { _ = storage.Close() })
	queue = openCheckpointLeaseQueue(t, storage)
	if err := queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("sweep after second restart: %v", err)
	}
	control, err = newPersistentControlRegistry(storage)
	if err != nil {
		t.Fatalf("reopen persistent controls: %v", err)
	}
	control.register("stable-worker")
	directives := deliveredControls(t, control, "stable-worker")
	if len(directives) != 1 || directives[0].Kind != yagocrawlcontract.CrawlControlCancel ||
		directives[0].RunID != testOrderRunID {
		t.Fatalf("replayed cancellation = %+v", directives)
	}
	adopted, err := queue.adoptWorkerSession(
		t.Context(),
		"stable-worker",
		"process-session-b",
	)
	if err != nil || len(adopted) != 1 || adopted[0].LeaseID != leaseID {
		t.Fatalf("restart adoption = %+v, err=%v", adopted, err)
	}
	record, found := leaseRecordFor(t, queue, leaseID)
	if !found || record.WorkerID != "stable-worker" ||
		record.WorkerSessionID != "process-session-b" ||
		record.ExpiresAtUnixNano != base.Add(5*time.Minute).UnixNano() {
		t.Fatalf("restart lease = %+v/%v", record, found)
	}
}

func openCheckpointLeaseStorage(t *testing.T, path string) *vault.Vault {
	t.Helper()
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open checkpoint lease storage: %v", err)
	}

	return storage
}

func openCheckpointLeaseQueue(t *testing.T, storage *vault.Vault) *DurableOrderQueue {
	t.Helper()
	queue, err := newDurableOrderQueue(storage, time.Minute)
	if err != nil {
		t.Fatalf("open checkpoint lease queue: %v", err)
	}

	return queue
}
