package crawlbroker

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type orderedBlockingProgressSink struct {
	mu             sync.Mutex
	order          []yagocrawlcontract.CrawlRunState
	runningEntered chan struct{}
	releaseRunning chan struct{}
}

func (sink *orderedBlockingProgressSink) Record(
	_ context.Context,
	progress yagocrawlcontract.CrawlRunProgress,
) {
	close(sink.runningEntered)
	<-sink.releaseRunning
	sink.mu.Lock()
	sink.order = append(sink.order, progress.State)
	sink.mu.Unlock()
}

func (sink *orderedBlockingProgressSink) RecordTerminal(
	_ context.Context,
	_ []byte,
	progress yagocrawlcontract.CrawlRunProgress,
) error {
	sink.mu.Lock()
	sink.order = append(sink.order, progress.State)
	sink.mu.Unlock()

	return nil
}

func (*orderedBlockingProgressSink) ConfirmTerminalDelivery(context.Context, []byte) error {
	return nil
}

func TestAuthorizedRunningProgressCannotOvertakeTerminalSettlement(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "progress-ordering", "worker")
	terminal := terminalOrderAcknowledgment(t, queue, leaseID, "worker", false)
	sink := &orderedBlockingProgressSink{
		runningEntered: make(chan struct{}),
		releaseRunning: make(chan struct{}),
	}
	server := newExchangeServer(queue, nil)
	server.progress = sink
	activateTestWorkerSession(t, server, terminal.GetWorkerId(), terminal.GetWorkerSessionId())
	runningDone := make(chan error, 1)
	go func() {
		_, err := server.ReportProgress(context.Background(), &crawlrpc.CrawlProgressReport{
			LeaseId:         leaseID,
			WorkerId:        terminal.GetWorkerId(),
			WorkerSessionId: terminal.GetWorkerSessionId(),
			RunId:           []byte("admin"),
			State:           crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING,
		})
		runningDone <- err
	}()
	select {
	case <-sink.runningEntered:
	case <-time.After(time.Second):
		t.Fatal("running progress did not reach sink")
	}
	if queue.leaseMutation.TryLock() {
		queue.leaseMutation.Unlock()
		t.Fatal("running progress released lease fence before sink return")
	}
	terminalDone := make(chan error, 1)
	go func() {
		_, err := server.AckOrder(context.Background(), terminal)
		terminalDone <- err
	}()
	close(sink.releaseRunning)
	if err := <-runningDone; err != nil {
		t.Fatalf("running progress: %v", err)
	}
	if err := <-terminalDone; err != nil {
		t.Fatalf("terminal settlement: %v", err)
	}
	sink.mu.Lock()
	order := append([]yagocrawlcontract.CrawlRunState(nil), sink.order...)
	sink.mu.Unlock()
	want := []yagocrawlcontract.CrawlRunState{
		yagocrawlcontract.CrawlRunRunning,
		yagocrawlcontract.CrawlRunFinished,
	}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("progress order = %v, want %v", order, want)
	}
}
