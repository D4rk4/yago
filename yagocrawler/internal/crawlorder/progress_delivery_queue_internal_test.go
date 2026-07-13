package crawlorder

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	grpc "google.golang.org/grpc"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type progressDeliveryClient struct {
	mu        sync.Mutex
	calls     []*crawlrpc.CrawlProgressReport
	errors    []error
	gate      <-chan struct{}
	started   chan *crawlrpc.CrawlProgressReport
	active    int
	maxActive int
}

func (c *progressDeliveryClient) ReportProgress(
	ctx context.Context,
	report *crawlrpc.CrawlProgressReport,
	_ ...grpc.CallOption,
) (*crawlrpc.CrawlProgressAck, error) {
	copyReport := &crawlrpc.CrawlProgressReport{
		WorkerId:      report.GetWorkerId(),
		RunId:         append([]byte(nil), report.GetRunId()...),
		ProfileHandle: report.GetProfileHandle(),
		ProfileName:   report.GetProfileName(),
		State:         report.GetState(),
		Tally: &crawlrpc.CrawlRunTally{
			Fetched:      report.GetTally().GetFetched(),
			Indexed:      report.GetTally().GetIndexed(),
			Failed:       report.GetTally().GetFailed(),
			RobotsDenied: report.GetTally().GetRobotsDenied(),
			Duplicates:   report.GetTally().GetDuplicates(),
			Pending:      report.GetTally().GetPending(),
		},
	}
	c.mu.Lock()
	c.calls = append(c.calls, copyReport)
	c.active++
	c.maxActive = max(c.maxActive, c.active)
	var result error
	if len(c.errors) > 0 {
		result = c.errors[0]
		c.errors = c.errors[1:]
	}
	c.mu.Unlock()
	if c.started != nil {
		c.started <- copyReport
	}
	if c.gate != nil {
		select {
		case <-c.gate:
		case <-ctx.Done():
			result = ctx.Err()
		}
	}
	c.mu.Lock()
	c.active--
	c.mu.Unlock()
	if result != nil {
		return nil, fmt.Errorf("report progress: %w", result)
	}

	return &crawlrpc.CrawlProgressAck{}, nil
}

func (c *progressDeliveryClient) snapshot() ([]*crawlrpc.CrawlProgressReport, int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]*crawlrpc.CrawlProgressReport(nil), c.calls...), c.maxActive
}

func testProgressDeliveryPolicy() progressDeliveryPolicy {
	return progressDeliveryPolicy{
		capacity:     8,
		rpcTimeout:   time.Second,
		retryMinimum: time.Millisecond,
		retryMaximum: 2 * time.Millisecond,
		entropy:      bytes.NewReader(make([]byte, 64)),
	}
}

func waitProgressCall(
	t *testing.T,
	started <-chan *crawlrpc.CrawlProgressReport,
) *crawlrpc.CrawlProgressReport {
	t.Helper()
	select {
	case report := <-started:
		return report
	case <-time.After(time.Second):
		t.Fatal("progress report was not attempted")

		return nil
	}
}

func TestProgressDeliveryCoalescesWithoutBlockingRunCompletion(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan *crawlrpc.CrawlProgressReport, 4)
	client := &progressDeliveryClient{gate: gate, started: started}
	queue := newProgressDeliveryQueue(client, "worker-1", testProgressDeliveryPolicy())
	provenance := []byte("run-1")
	returned := make(chan struct{})
	go func() {
		queue.enqueue(context.Background(), RunReport{
			Provenance: provenance,
			State:      yagocrawlcontract.CrawlRunRunning,
			Tally:      yagocrawlcontract.CrawlRunTally{Indexed: 1},
		})
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("enqueue blocked on the progress RPC")
	}
	provenance[0] = 'x'
	first := waitProgressCall(t, started)
	if string(first.GetRunId()) != "run-1" {
		t.Fatalf("first run id = %q", first.GetRunId())
	}
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("run-1"),
		State:      yagocrawlcontract.CrawlRunRunning,
		Tally:      yagocrawlcontract.CrawlRunTally{Indexed: 2},
	})
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("run-1"),
		State:      yagocrawlcontract.CrawlRunFinished,
		Tally:      yagocrawlcontract.CrawlRunTally{Indexed: 3},
	})
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("run-1"),
		State:      yagocrawlcontract.CrawlRunRunning,
		Tally:      yagocrawlcontract.CrawlRunTally{Indexed: 4},
	})
	close(gate)
	if err := queue.close(t.Context()); err != nil {
		t.Fatalf("close queue: %v", err)
	}
	calls, maxActive := client.snapshot()
	if len(calls) != 3 || calls[1].GetState() != crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING ||
		calls[1].GetTally().GetIndexed() != 2 ||
		calls[2].GetState() != crawlrpc.CrawlRunState_CRAWL_RUN_STATE_FINISHED ||
		calls[2].GetTally().GetIndexed() != 3 {
		t.Fatalf("coalesced calls = %+v", calls)
	}
	if maxActive != 1 {
		t.Fatalf("maximum concurrent progress calls = %d, want 1", maxActive)
	}
	if err := queue.close(t.Context()); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestProgressDeliveryPreservesRepeatedRunTransitions(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan *crawlrpc.CrawlProgressReport, 4)
	client := &progressDeliveryClient{gate: gate, started: started}
	queue := newProgressDeliveryQueue(client, "worker-retry", testProgressDeliveryPolicy())
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("repeated"), State: yagocrawlcontract.CrawlRunCancelled,
	})
	waitProgressCall(t, started)
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("repeated"), State: yagocrawlcontract.CrawlRunRunning,
	})
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("repeated"), State: yagocrawlcontract.CrawlRunFinished,
	})
	queue.mu.Lock()
	queued := len(queue.pending["repeated"])
	queue.mu.Unlock()
	if queued != 3 {
		t.Fatalf("queued transitions = %d, want 3", queued)
	}
	close(gate)
	if err := queue.close(t.Context()); err != nil {
		t.Fatalf("close queue: %v", err)
	}
	calls, _ := client.snapshot()
	if len(calls) != 3 ||
		calls[0].GetState() != crawlrpc.CrawlRunState_CRAWL_RUN_STATE_CANCELLED ||
		calls[1].GetState() != crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING ||
		calls[2].GetState() != crawlrpc.CrawlRunState_CRAWL_RUN_STATE_FINISHED {
		t.Fatalf("repeated transitions = %+v", calls)
	}
}

func TestProgressDeliveryPrioritizesReadyTerminalHeads(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan *crawlrpc.CrawlProgressReport, 8)
	client := &progressDeliveryClient{gate: gate, started: started}
	queue := newProgressDeliveryQueue(client, "worker-priority", testProgressDeliveryPolicy())
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("in-flight"), State: yagocrawlcontract.CrawlRunRunning,
	})
	waitProgressCall(t, started)
	for _, runID := range []string{"running-a", "running-b", "running-c"} {
		queue.enqueue(context.Background(), RunReport{
			Provenance: []byte(runID), State: yagocrawlcontract.CrawlRunRunning,
		})
	}
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("terminal"), State: yagocrawlcontract.CrawlRunFinished,
	})
	close(gate)
	second := waitProgressCall(t, started)
	if string(second.GetRunId()) != "terminal" {
		t.Fatalf("second report = %q, want terminal", second.GetRunId())
	}
	if err := queue.close(t.Context()); err != nil {
		t.Fatalf("close queue: %v", err)
	}
}

func TestProgressDeliveryDoesNotWaitBehindTerminalBackoff(t *testing.T) {
	started := make(chan *crawlrpc.CrawlProgressReport, 4)
	client := &progressDeliveryClient{
		errors:  []error{errors.New("terminal failed")},
		started: started,
	}
	policy := testProgressDeliveryPolicy()
	policy.retryMinimum = time.Second
	policy.retryMaximum = time.Second
	queue := newProgressDeliveryQueue(client, "worker-backoff", policy)
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("terminal"), State: yagocrawlcontract.CrawlRunFinished,
	})
	waitProgressCall(t, started)
	deadline := time.Now().Add(time.Second)
	for {
		queue.mu.Lock()
		sequence := queue.pending["terminal"]
		waiting := len(sequence) == 1 && sequence[0].due.After(time.Now())
		queue.mu.Unlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("terminal report did not enter backoff")
		}
		time.Sleep(time.Millisecond)
	}
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("running"), State: yagocrawlcontract.CrawlRunRunning,
	})
	second := waitProgressCall(t, started)
	if string(second.GetRunId()) != "running" {
		t.Fatalf("second report = %q, want ready running", second.GetRunId())
	}
	queue.cancel()
	<-queue.done
	if err := queue.close(t.Context()); err != nil {
		t.Fatalf("close queue: %v", err)
	}
}

func TestProgressDeliveryDropsRunningFailureAndRetriesTerminal(t *testing.T) {
	started := make(chan *crawlrpc.CrawlProgressReport, 4)
	client := &progressDeliveryClient{
		errors:  []error{errors.New("running failed"), errors.New("terminal failed"), nil},
		started: started,
	}
	queue := newProgressDeliveryQueue(client, "worker-2", testProgressDeliveryPolicy())
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("running"),
		State:      yagocrawlcontract.CrawlRunRunning,
	})
	waitProgressCall(t, started)
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("terminal"),
		State:      yagocrawlcontract.CrawlRunCancelled,
	})
	waitProgressCall(t, started)
	last := waitProgressCall(t, started)
	if string(last.GetRunId()) != "terminal" ||
		last.GetState() != crawlrpc.CrawlRunState_CRAWL_RUN_STATE_CANCELLED {
		t.Fatalf("retried report = %+v", last)
	}
	if err := queue.close(t.Context()); err != nil {
		t.Fatalf("close queue: %v", err)
	}
	calls, _ := client.snapshot()
	if len(calls) != 3 {
		t.Fatalf("progress calls = %d, want one running and two terminal", len(calls))
	}
}

func TestProgressDeliveryCapacityPrefersTerminalStates(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan *crawlrpc.CrawlProgressReport, 8)
	client := &progressDeliveryClient{gate: gate, started: started}
	policy := testProgressDeliveryPolicy()
	policy.capacity = 2
	queue := newProgressDeliveryQueue(client, "worker-3", policy)
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("running-a"), State: yagocrawlcontract.CrawlRunRunning,
	})
	waitProgressCall(t, started)
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("running-b"), State: yagocrawlcontract.CrawlRunRunning,
	})
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("terminal-c"), State: yagocrawlcontract.CrawlRunFinished,
	})
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("terminal-d"), State: yagocrawlcontract.CrawlRunCancelled,
	})
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("terminal-e"), State: yagocrawlcontract.CrawlRunFinished,
	})

	queue.mu.Lock()
	_, hasC := queue.pending["terminal-c"]
	_, hasD := queue.pending["terminal-d"]
	_, hasE := queue.pending["terminal-e"]
	queue.mu.Unlock()
	if !hasC || !hasD || hasE {
		t.Fatalf("pending terminal set = c:%t d:%t e:%t", hasC, hasD, hasE)
	}
	close(gate)
	if err := queue.close(t.Context()); err != nil {
		t.Fatalf("close queue: %v", err)
	}
	calls, _ := client.snapshot()
	if len(calls) != 3 || string(calls[1].GetRunId()) != "terminal-c" ||
		string(calls[2].GetRunId()) != "terminal-d" {
		t.Fatalf("capacity calls = %+v", calls)
	}
}

func TestProgressDeliveryCapacityKeepsAttemptTransitionsWhole(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan *crawlrpc.CrawlProgressReport, 4)
	client := &progressDeliveryClient{gate: gate, started: started}
	policy := testProgressDeliveryPolicy()
	policy.capacity = 3
	queue := newProgressDeliveryQueue(client, "worker-capacity", policy)
	for _, state := range []yagocrawlcontract.CrawlRunState{
		yagocrawlcontract.CrawlRunFinished,
		yagocrawlcontract.CrawlRunRunning,
		yagocrawlcontract.CrawlRunCancelled,
	} {
		queue.enqueue(context.Background(), RunReport{
			Provenance: []byte("repeated"), State: state,
		})
		if state == yagocrawlcontract.CrawlRunFinished {
			waitProgressCall(t, started)
		}
	}
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("overflow"), State: yagocrawlcontract.CrawlRunFinished,
	})
	queue.mu.Lock()
	repeated := len(queue.pending["repeated"])
	_, overflow := queue.pending["overflow"]
	queue.mu.Unlock()
	if repeated != 3 || overflow {
		t.Fatalf("capacity state = repeated:%d overflow:%t", repeated, overflow)
	}
	close(gate)
	if err := queue.close(t.Context()); err != nil {
		t.Fatalf("close queue: %v", err)
	}
	calls, _ := client.snapshot()
	if len(calls) != 3 ||
		calls[0].GetState() != crawlrpc.CrawlRunState_CRAWL_RUN_STATE_FINISHED ||
		calls[1].GetState() != crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING ||
		calls[2].GetState() != crawlrpc.CrawlRunState_CRAWL_RUN_STATE_CANCELLED {
		t.Fatalf("capacity transitions = %+v", calls)
	}
}

func TestProgressDeliveryCloseDeadlineCancelsBlockedTerminal(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan *crawlrpc.CrawlProgressReport, 1)
	client := &progressDeliveryClient{gate: gate, started: started}
	queue := newProgressDeliveryQueue(client, "worker-4", testProgressDeliveryPolicy())
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("terminal"), State: yagocrawlcontract.CrawlRunFinished,
	})
	waitProgressCall(t, started)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := queue.close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close error = %v, want deadline", err)
	}
}

func TestProgressDeliveryCloseDropsQueuedRunningState(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan *crawlrpc.CrawlProgressReport, 2)
	client := &progressDeliveryClient{gate: gate, started: started}
	queue := newProgressDeliveryQueue(client, "worker-running", testProgressDeliveryPolicy())
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("in-flight"), State: yagocrawlcontract.CrawlRunRunning,
	})
	waitProgressCall(t, started)
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("queued"), State: yagocrawlcontract.CrawlRunRunning,
	})
	closed := make(chan error, 1)
	go func() { closed <- queue.close(t.Context()) }()
	deadline := time.After(time.Second)
	for {
		queue.mu.Lock()
		closing := queue.closed
		queue.mu.Unlock()
		if closing {
			break
		}
		select {
		case <-deadline:
			t.Fatal("progress queue did not enter closing state")
		case <-time.After(time.Millisecond):
		}
	}
	close(gate)
	if err := <-closed; err != nil {
		t.Fatalf("close queue: %v", err)
	}
	calls, _ := client.snapshot()
	if len(calls) != 1 || string(calls[0].GetRunId()) != "in-flight" {
		t.Fatalf("running calls after close = %+v", calls)
	}
}

type failingProgressEntropy struct{}

func (failingProgressEntropy) Read([]byte) (int, error) {
	return 0, errors.New("entropy failed")
}

type contextIgnoringProgressClient struct {
	started chan struct{}
	release chan struct{}
}

func (c contextIgnoringProgressClient) ReportProgress(
	context.Context,
	*crawlrpc.CrawlProgressReport,
	...grpc.CallOption,
) (*crawlrpc.CrawlProgressAck, error) {
	close(c.started)
	<-c.release

	return &crawlrpc.CrawlProgressAck{}, nil
}

func TestProgressDeliveryCloseDeadlineDoesNotJoinUncooperativeClient(t *testing.T) {
	client := contextIgnoringProgressClient{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	queue := newProgressDeliveryQueue(client, "worker-uncooperative", testProgressDeliveryPolicy())
	queue.enqueue(t.Context(), RunReport{
		Provenance: []byte("terminal"), State: yagocrawlcontract.CrawlRunFinished,
	})
	select {
	case <-client.started:
	case <-time.After(time.Second):
		t.Fatal("uncooperative client did not start")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	returned := make(chan error, 1)
	go func() { returned <- queue.close(ctx) }()
	select {
	case err := <-returned:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("close error = %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("close joined a context-ignoring client")
	}
	close(client.release)
	select {
	case <-queue.done:
	case <-time.After(time.Second):
		t.Fatal("progress worker did not quiesce after client release")
	}
}

func TestProgressDeliveryRetryDelayAndTerminalClassification(t *testing.T) {
	wait := 20 * time.Millisecond
	delay := progressRetryDelay(wait, bytes.NewReader(make([]byte, 8)))
	if delay < wait/2 || delay >= wait {
		t.Fatalf("retry delay = %v", delay)
	}
	if fallback := progressRetryDelay(wait, failingProgressEntropy{}); fallback != wait/2 {
		t.Fatalf("fallback delay = %v, want %v", fallback, wait/2)
	}
	if !terminalProgressState(yagocrawlcontract.CrawlRunFinished) ||
		!terminalProgressState(yagocrawlcontract.CrawlRunCancelled) ||
		terminalProgressState(yagocrawlcontract.CrawlRunRunning) {
		t.Fatal("terminal state classification mismatch")
	}
	timer := time.NewTimer(time.Hour)
	stopProgressTimer(timer)
	expired := time.NewTimer(0)
	<-expired.C
	stopProgressTimer(expired)
}

func TestProgressDeliveryWorkerCanBeCancelledWhileIdle(t *testing.T) {
	queue := newProgressDeliveryQueue(
		&progressDeliveryClient{},
		"worker-5",
		testProgressDeliveryPolicy(),
	)
	queue.cancel()
	select {
	case <-queue.done:
	case <-time.After(time.Second):
		t.Fatal("idle progress worker did not stop")
	}
	if err := queue.close(t.Context()); err != nil {
		t.Fatalf("close cancelled queue: %v", err)
	}
}
