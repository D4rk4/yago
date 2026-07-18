package crawlbroker

import (
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestAuthorizedProgressDoesNotRescanWorkerLeases(t *testing.T) {
	fixture := scriptedQueue(t)
	leaseID := leaseOneForSession(
		t,
		fixture.queue,
		"authorized-progress",
		"worker",
		testWorkerSessionID,
	)
	server := newExchangeServer(fixture.queue, nil)
	sink := &recordingProgressSink{}
	server.progress = sink
	activateTestWorkerSession(t, server, "worker", testWorkerSessionID)
	fixture.engine.scanErrors[leaseBucket] = errors.New("unexpected lease scan")
	if _, err := server.ReportProgress(t.Context(), &crawlrpc.CrawlProgressReport{
		WorkerId:        "worker",
		WorkerSessionId: testWorkerSessionID,
		LeaseId:         leaseID,
		RunId:           []byte("admin"),
		State:           crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING,
	}); err != nil {
		t.Fatalf("report authorized progress: %v", err)
	}
	if sink.n != 1 {
		t.Fatalf("authorized progress records = %d, want 1", sink.n)
	}
}

func TestAuthorizedTerminalProgressDoesNotRescanWorkerLeases(t *testing.T) {
	fixture := scriptedQueue(t)
	leaseID := leaseOneForSession(
		t,
		fixture.queue,
		"authorized-terminal-progress",
		"worker",
		testWorkerSessionID,
	)
	server := newExchangeServer(fixture.queue, nil)
	sink := &recordingProgressSink{}
	server.progress = sink
	activateTestWorkerSession(t, server, "worker", testWorkerSessionID)
	fixture.engine.scanErrors[leaseBucket] = errors.New("unexpected lease scan")
	if _, err := server.ReportProgress(t.Context(), &crawlrpc.CrawlProgressReport{
		WorkerId:        "worker",
		WorkerSessionId: testWorkerSessionID,
		LeaseId:         leaseID,
		RunId:           []byte("admin"),
		State:           crawlrpc.CrawlRunState_CRAWL_RUN_STATE_FINISHED,
	}); err != nil {
		t.Fatalf("report authorized terminal progress: %v", err)
	}
	if sink.n != 1 || sink.last.State != yagocrawlcontract.CrawlRunFinished {
		t.Fatalf("authorized terminal progress = %d/%+v", sink.n, sink.last)
	}
}

func TestAuthorizedProgressReassignsPendingDirectiveWithoutLeaseScan(t *testing.T) {
	fixture := scriptedQueue(t)
	leaseID := leaseOneForSession(
		t,
		fixture.queue,
		"authorized-control-progress",
		"worker",
		testWorkerSessionID,
	)
	registry, err := newPersistentControlRegistry(fixture.queue.vault)
	if err != nil {
		t.Fatalf("open persistent control registry: %v", err)
	}
	if !registry.Enqueue("previous-worker", yagocrawlcontract.CrawlControlDirective{
		Kind:  yagocrawlcontract.CrawlControlCancel,
		RunID: testOrderRunID,
	}) {
		t.Fatal("enqueue pending run directive")
	}
	server := newExchangeServer(fixture.queue, nil)
	server.control = registry
	sink := &recordingProgressSink{}
	server.progress = sink
	activateTestWorkerSession(t, server, "worker", testWorkerSessionID)
	fixture.engine.scanErrors[leaseBucket] = errors.New("unexpected lease scan")
	if _, err := server.ReportProgress(t.Context(), &crawlrpc.CrawlProgressReport{
		WorkerId:        "worker",
		WorkerSessionId: testWorkerSessionID,
		LeaseId:         leaseID,
		RunId:           []byte("admin"),
		State:           crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING,
	}); err != nil {
		t.Fatalf("report authorized control progress: %v", err)
	}
	if previous := deliveredControls(t, registry, "previous-worker"); len(previous) != 0 {
		t.Fatalf("previous worker directives = %+v", previous)
	}
	moved := deliveredControls(t, registry, "worker")
	if len(moved) != 1 || moved[0].RunID != testOrderRunID ||
		moved[0].Kind != yagocrawlcontract.CrawlControlCancel {
		t.Fatalf("authorized worker directives = %+v", moved)
	}
	if sink.n != 1 {
		t.Fatalf("authorized control progress records = %d, want 1", sink.n)
	}
}
