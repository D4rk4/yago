package crawlorder

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yagocrawler/internal/frontier"
)

type recordingProgress struct {
	mu       sync.Mutex
	running  int
	pendings []uint64
	terminal bool
	signal   chan struct{}
}

func (r *recordingProgress) ReportRun(_ context.Context, report RunReport) {
	r.mu.Lock()
	if report.State == yagocrawlcontract.CrawlRunRunning {
		r.running++
		r.pendings = append(r.pendings, report.Tally.Pending)
	} else {
		r.terminal = true
	}
	r.mu.Unlock()
	select {
	case r.signal <- struct{}{}:
	default:
	}
}

func (r *recordingProgress) runningCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.running
}

func (r *recordingProgress) sawTerminal() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.terminal
}

func waitForRunning(t *testing.T, p *recordingProgress, want int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for p.runningCount() < want {
		select {
		case <-p.signal:
		case <-deadline:
			t.Fatalf("only %d running reports arrived, want %d", p.runningCount(), want)
		}
	}
}

func TestAcceptEmitsPeriodicRunningReports(t *testing.T) {
	restore := progressReportInterval
	t.Cleanup(func() { progressReportInterval = restore })
	progressReportInterval = time.Millisecond

	f := frontier.NewFrontier(4, nil)
	profile := consumerProfile()
	progress := &recordingProgress{signal: make(chan struct{}, 64)}
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
		f,
	).WithProgressReporter(progress)

	acked := make(chan struct{})
	consumer.accept(context.Background(), CrawlOrderDelivery{
		Order: yagocrawlcontract.CrawlOrder{
			Profile:    profile,
			Provenance: []byte("run-1"),
			Requests: []yagocrawlcontract.CrawlRequest{{
				URL:           "https://example.org/a",
				ProfileHandle: profile.Handle,
			}},
		},
		Ack: func(context.Context) error {
			close(acked)

			return nil
		},
	})

	// accept sends exactly one start report synchronously, so a second running
	// report proves the periodic ticker fired while the run was in flight.
	waitForRunning(t, progress, 2)

	job := <-f.Jobs()
	f.Done(job, false)
	waitCallback(t, acked)

	if !progress.sawTerminal() {
		t.Fatal("expected a terminal report after the run drained")
	}
	progress.mu.Lock()
	defer progress.mu.Unlock()
	for _, pending := range progress.pendings {
		if pending == 1 {
			return
		}
	}
	t.Fatalf("no running report carried the live pending of 1: %v", progress.pendings)
}
