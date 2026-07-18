package crawlbroker

import (
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

const testOrderRunID = "61646d696e"

type staleProgressFixture struct {
	server       *exchangeServer
	registry     *ControlRegistry
	sink         *recordingProgressSink
	workerALease string
	workerBLease string
}

func TestStaleWorkerProgressCannotConsumeNewLeaseControl(t *testing.T) {
	fixture := newStaleProgressFixture(t)
	assertStaleProgressRejected(t, fixture)
	assertCurrentProgressConsumesControl(t, fixture)
	assertSettledProgressRejected(t, fixture)
}

func newStaleProgressFixture(t *testing.T) staleProgressFixture {
	t.Helper()
	set := withClock(t)
	base := time.Unix(20_000, 0)
	set(base)
	storage, err := boltvault.Open(filepath.Join(t.TempDir(), "node.db"), 0)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	queue, err := newDurableOrderQueue(storage, time.Minute)
	if err != nil {
		t.Fatalf("open order queue: %v", err)
	}
	registry, err := newPersistentControlRegistry(storage)
	if err != nil {
		t.Fatalf("open control registry: %v", err)
	}
	workerALease := leaseOne(t, queue, "fenced", "worker-a")
	set(base.Add(time.Minute))
	if err := queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("expire worker-a lease: %v", err)
	}
	_, workerBLease, found, err := queue.leasePopForSession(
		t.Context(),
		"worker-b",
		"session-b",
	)
	if err != nil || !found {
		t.Fatalf("lease to worker-b: found=%v err=%v", found, err)
	}
	registry.register("worker-b")
	if !registry.Enqueue("worker-b", yagocrawlcontract.CrawlControlDirective{
		Kind:  yagocrawlcontract.CrawlControlCancel,
		RunID: testOrderRunID,
	}) {
		t.Fatal("enqueue worker-b cancellation")
	}
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	server.control = registry
	sink := &recordingProgressSink{}
	server.progress = sink
	activateTestWorkerSession(t, server, "worker-b", "session-b")

	return staleProgressFixture{
		server:       server,
		registry:     registry,
		sink:         sink,
		workerALease: workerALease,
		workerBLease: workerBLease,
	}
}

func assertStaleProgressRejected(t *testing.T, fixture staleProgressFixture) {
	t.Helper()
	for _, state := range []crawlrpc.CrawlRunState{
		crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING,
		crawlrpc.CrawlRunState_CRAWL_RUN_STATE_FINISHED,
	} {
		if _, err := fixture.server.ReportProgress(t.Context(), &crawlrpc.CrawlProgressReport{
			LeaseId:         fixture.workerALease,
			WorkerId:        "worker-a",
			WorkerSessionId: "session-a",
			RunId:           []byte("admin"),
			State:           state,
		}); status.Code(err) != codes.FailedPrecondition {
			t.Fatalf(
				"stale worker progress %v status = %v, want FailedPrecondition",
				state,
				status.Code(err),
			)
		}
	}
	if fixture.sink.n != 0 {
		t.Fatalf("stale progress records = %d, want none", fixture.sink.n)
	}
	if stale := deliveredControls(t, fixture.registry, "worker-a"); len(stale) != 0 {
		t.Fatalf("stale worker directives = %+v, want none", stale)
	}
	pending := deliveredControls(t, fixture.registry, "worker-b")
	if len(pending) != 1 || pending[0].Kind != yagocrawlcontract.CrawlControlCancel {
		t.Fatalf("worker-b directives = %+v, want cancellation", pending)
	}
}

func assertCurrentProgressConsumesControl(t *testing.T, fixture staleProgressFixture) {
	t.Helper()
	if _, err := fixture.server.ReportProgress(t.Context(), &crawlrpc.CrawlProgressReport{
		LeaseId:         fixture.workerBLease,
		WorkerId:        "worker-b",
		WorkerSessionId: "session-b",
		RunId:           []byte("admin"),
		State:           crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING,
	}); err != nil {
		t.Fatalf("current worker progress: %v", err)
	}
	if fixture.sink.n != 1 || fixture.sink.last.WorkerID != "worker-b" {
		t.Fatalf("current worker progress = %d/%+v", fixture.sink.n, fixture.sink.last)
	}
	if _, err := fixture.server.AckOrder(t.Context(), &crawlrpc.OrderAck{
		LeaseId: fixture.workerBLease, WorkerId: "worker-b", WorkerSessionId: "session-b",
	}); err != nil {
		t.Fatalf("ack worker-b order: %v", err)
	}
	if remaining := deliveredControls(t, fixture.registry, "worker-b"); len(remaining) != 0 {
		t.Fatalf("terminal directives = %+v, want none", remaining)
	}
}

func assertSettledProgressRejected(t *testing.T, fixture staleProgressFixture) {
	t.Helper()
	if _, err := fixture.server.ReportProgress(t.Context(), &crawlrpc.CrawlProgressReport{
		LeaseId:         fixture.workerALease,
		WorkerId:        "worker-a",
		WorkerSessionId: "session-a",
		RunId:           []byte("admin"),
		State:           crawlrpc.CrawlRunState_CRAWL_RUN_STATE_FINISHED,
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("delayed stale progress status = %v, want FailedPrecondition", status.Code(err))
	}
	if fixture.sink.n != 1 {
		t.Fatalf("delayed stale progress records = %d, want one current record", fixture.sink.n)
	}
	if _, err := fixture.server.ReportProgress(t.Context(), &crawlrpc.CrawlProgressReport{
		LeaseId:         fixture.workerBLease,
		WorkerId:        "worker-b",
		WorkerSessionId: "session-b",
		RunId:           []byte("admin"),
		State:           crawlrpc.CrawlRunState_CRAWL_RUN_STATE_FINISHED,
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("settled lease progress status = %v, want FailedPrecondition", status.Code(err))
	}
	if fixture.sink.n != 1 || fixture.sink.last.WorkerID != "worker-b" {
		t.Fatalf(
			"settled lease progress mutated sink = %d/%+v",
			fixture.sink.n,
			fixture.sink.last,
		)
	}
}

func TestAcknowledgedOrderControlCleanupReplaysAfterBrokerCrash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open first storage: %v", err)
	}
	firstQueue, err := newDurableOrderQueue(storage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("open first order queue: %v", err)
	}
	firstRegistry, err := newPersistentControlRegistry(storage)
	if err != nil {
		t.Fatalf("open first control registry: %v", err)
	}
	firstRegistry.register("worker")
	leaseID := leaseOne(t, firstQueue, "crash", "worker")
	if !firstRegistry.Enqueue("worker", yagocrawlcontract.CrawlControlDirective{
		Kind:  yagocrawlcontract.CrawlControlCancel,
		RunID: testOrderRunID,
	}) {
		t.Fatal("enqueue cancellation")
	}
	target, err := firstQueue.ackLeaseWithTarget(t.Context(), leaseID)
	if err != nil {
		t.Fatalf("persist order acknowledgment: %v", err)
	}
	if target.WorkerID != "worker" || target.RunID != testOrderRunID {
		t.Fatalf("control cleanup target = %+v", target)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close first storage: %v", err)
	}

	storage, err = boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open second storage: %v", err)
	}
	queue, err := newDurableOrderQueue(storage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("open second order queue: %v", err)
	}
	registry, err := newPersistentControlRegistry(storage)
	if err != nil {
		t.Fatalf("open second control registry: %v", err)
	}
	if pending := deliveredControls(t, registry, "worker"); len(pending) != 1 {
		t.Fatalf("control before replay = %+v, want one", pending)
	}
	if err := queue.replayRunControlCompletions(t.Context(), registry); err != nil {
		t.Fatalf("replay cleanup: %v", err)
	}
	if pending := deliveredControls(t, registry, "worker"); len(pending) != 0 {
		t.Fatalf("control after replay = %+v, want none", pending)
	}
	if outbox, err := queue.pendingRunControlCompletions(
		t.Context(),
	); err != nil ||
		len(outbox) != 0 {
		t.Fatalf("cleanup outbox = %+v err=%v, want empty", outbox, err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close second storage: %v", err)
	}

	storage, err = boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open third storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	registry, err = newPersistentControlRegistry(storage)
	if err != nil {
		t.Fatalf("open third control registry: %v", err)
	}
	if pending := deliveredControls(t, registry, "worker"); len(pending) != 0 {
		t.Fatalf("reopened controls = %+v, want none", pending)
	}
}
