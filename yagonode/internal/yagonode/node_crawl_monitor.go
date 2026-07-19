package yagonode

import (
	"context"
	"strconv"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/crawlruns"
)

// crawlRunRegistry returns the crawl run registry when the runtime is a live crawl
// runtime, or nil when crawling is disabled (or the runtime is a test double), so
// the console's crawl monitor is wired only when there are runs to observe.
func crawlRunRegistry(runtime crawlProcess) *crawlruns.Registry {
	provider, ok := runtime.(interface {
		runRegistry() *crawlruns.Registry
	})
	if !ok {
		return nil
	}

	return provider.runRegistry()
}

// crawlMonitorSource adapts the node's crawl run registry and order-queue probe to
// the console's read-only crawl monitor.
type crawlMonitorSource struct {
	runs  *crawlruns.Registry
	probe func(context.Context) (crawlbroker.QueueDepth, error)
	now   func() time.Time
}

func newCrawlMonitorSource(
	runs *crawlruns.Registry,
	probe func(context.Context) (crawlbroker.QueueDepth, error),
) *crawlMonitorSource {
	return &crawlMonitorSource{runs: runs, probe: probe, now: time.Now}
}

// Monitor snapshots the known runs newest-first and, when a probe is available,
// the broker's outstanding order backlog. A probe error degrades to a zero
// backlog rather than failing the whole monitor render.
func (s *crawlMonitorSource) Monitor(ctx context.Context) adminui.CrawlMonitor {
	runs := s.runs.Recent()
	monitor := adminui.CrawlMonitor{Runs: make([]adminui.CrawlRunView, 0, len(runs))}
	now := s.now()
	for _, run := range runs {
		monitor.Runs = append(monitor.Runs, crawlRunView(run, now))
		monitor.Totals.Fetched = crawlTallyTotal(monitor.Totals.Fetched, run.Tally.Fetched)
		monitor.Totals.Indexed = crawlTallyTotal(monitor.Totals.Indexed, run.Tally.Indexed)
		monitor.Totals.Failed = crawlTallyTotal(monitor.Totals.Failed, run.Tally.Failed)
		monitor.Totals.RobotsDenied = crawlTallyTotal(
			monitor.Totals.RobotsDenied,
			run.Tally.RobotsDenied,
		)
		monitor.Totals.Duplicates = crawlTallyTotal(
			monitor.Totals.Duplicates,
			run.Tally.Duplicates,
		)
	}
	if s.probe != nil {
		if depth, err := s.probe(ctx); err == nil {
			monitor.QueueAvailable = true
			monitor.QueuePending = depth.Pending
			monitor.QueueLeased = depth.Leased
		}
	}

	return monitor
}

func crawlRunView(run crawlruns.Run, now time.Time) adminui.CrawlRunView {
	end := run.Updated
	if run.State != yagocrawlcontract.CrawlRunFinished &&
		run.State != yagocrawlcontract.CrawlRunCancelled {
		end = now
	}
	runtime := end.Sub(run.FirstSeen)
	if runtime < 0 {
		runtime = 0
	}

	return adminui.CrawlRunView{
		RunID:           run.RunID,
		Profile:         crawlRunLabel(run),
		Worker:          run.WorkerID,
		State:           string(run.State),
		Fetched:         run.Tally.Fetched,
		Indexed:         run.Tally.Indexed,
		Failed:          run.Tally.Failed,
		RobotsDenied:    run.Tally.RobotsDenied,
		Duplicates:      run.Tally.Duplicates,
		Pending:         run.Tally.Pending,
		Runtime:         runtime.Round(time.Second).String(),
		PagesPerMinute:  run.PagesPerMinute,
		RateKnown:       run.RateKnown,
		MaxPagesPerHost: crawlRunPerHostLimit(run),
		MaxPagesPerRun:  crawlRunWholeLimit(run),
	}
}

func crawlRunPerHostLimit(run crawlruns.Run) string {
	if !run.LimitsKnown {
		return "Unavailable"
	}
	if run.MaxPagesPerHost == yagocrawlcontract.UnlimitedPagesPerHost {
		return "Unlimited"
	}

	return strconv.Itoa(run.MaxPagesPerHost)
}

func crawlRunWholeLimit(run crawlruns.Run) string {
	if !run.LimitsKnown {
		return "Unavailable"
	}
	if run.MaxPagesPerRun == 0 {
		return "Unlimited"
	}

	return strconv.Itoa(run.MaxPagesPerRun)
}
