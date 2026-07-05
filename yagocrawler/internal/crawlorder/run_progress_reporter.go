package crawlorder

import (
	"context"
	"sync"
	"time"
)

// progressReportInterval spaces the running-state reports a worker sends between a
// run's start and terminal reports, so the node's crawl monitor shows a live tally
// and an advancing runtime instead of the frozen start snapshot. It is a var so
// tests can shorten it.
var progressReportInterval = 5 * time.Second

// runProgressReporter periodically re-reports a running crawl until the run
// finishes. The report closure is attached after the run is seeded (via reportWith)
// so it can read the run's live pending count. Stop is called from the run's
// completion callback; it marks the run finished under the same lock that guards a
// report, so a tick already past its finished check waits behind the report and no
// running-state report can race past the terminal report the completion callback
// then sends.
type runProgressReporter struct {
	stop chan struct{}
	once sync.Once

	mu       sync.Mutex
	report   func()
	finished bool
}

func newRunProgressReporter() *runProgressReporter {
	return &runProgressReporter{stop: make(chan struct{})}
}

// reportWith attaches the report closure invoked on each tick. It is called once,
// before start, once the seeded run's identity is known.
func (r *runProgressReporter) reportWith(report func()) {
	r.mu.Lock()
	r.report = report
	r.mu.Unlock()
}

// start launches the periodic reporter and returns immediately; the goroutine
// exits when Stop is called or ctx is cancelled.
func (r *runProgressReporter) start(ctx context.Context, interval time.Duration) {
	go r.run(ctx, interval)
}

func (r *runProgressReporter) run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stop:
			return
		case <-ticker.C:
			r.reportRunning()
		}
	}
}

// reportRunning emits one running-state report unless the run has finished or no
// report closure is attached yet.
func (r *runProgressReporter) reportRunning() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.finished && r.report != nil {
		r.report()
	}
}

// Stop ends periodic reporting and marks the run finished so a concurrent tick
// cannot emit a running report after the caller's terminal report.
func (r *runProgressReporter) Stop() {
	r.once.Do(func() {
		r.mu.Lock()
		r.finished = true
		r.mu.Unlock()
		close(r.stop)
	})
}
