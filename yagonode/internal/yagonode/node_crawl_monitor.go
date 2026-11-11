package yagonode

import (
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
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

type crawlMonitorSource struct {
	runs *crawlruns.Registry
	now  func() time.Time
}

func newCrawlMonitorSource(runs *crawlruns.Registry) *crawlMonitorSource {
	return &crawlMonitorSource{runs: runs, now: time.Now}
}

func (s *crawlMonitorSource) Monitor(_ context.Context) adminui.CrawlMonitor {
	runs := s.runs.Recent()
	monitor := adminui.CrawlMonitor{Runs: make([]adminui.CrawlRunView, 0, len(runs))}
	now := s.now()
	for _, run := range runs {
		runView := crawlRunView(run, now)
		monitor.Runs = append(monitor.Runs, runView)
		if runView.State == string(yagocrawlcontract.CrawlRunRunning) {
			monitor.PendingFrontierURLs = crawlTallyTotal(
				monitor.PendingFrontierURLs,
				run.Tally.Pending,
			)
		}
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
		monitor.Totals.DocumentCandidates = crawlTallyTotal(
			monitor.Totals.DocumentCandidates,
			run.Tally.DocumentCandidates,
		)
		monitor.Totals.RemovalMutations = crawlTallyTotal(
			monitor.Totals.RemovalMutations,
			run.Tally.RemovalMutations,
		)
		monitor.Totals.NotIndexable = crawlTallyTotal(
			monitor.Totals.NotIndexable,
			run.Tally.NotIndexable,
		)
		monitor.Totals.NodeRejected = crawlTallyTotal(
			monitor.Totals.NodeRejected,
			run.Tally.NodeRejected,
		)
		monitor.Totals.PendingIngest = crawlTallyTotal(
			monitor.Totals.PendingIngest,
			run.Tally.PendingIngest,
		)
		knownOutcomes := crawlTallyTotal(run.Tally.Indexed, run.Tally.PendingIngest)
		knownOutcomes = crawlTallyTotal(knownOutcomes, run.Tally.NotIndexable)
		knownOutcomes = crawlTallyTotal(knownOutcomes, run.Tally.NodeRejected)
		if run.Tally.Fetched > knownOutcomes {
			monitor.Totals.UnknownOutcome = crawlTallyTotal(
				monitor.Totals.UnknownOutcome,
				run.Tally.Fetched-knownOutcomes,
			)
		}
		for _, outcome := range run.Tally.OutcomeCounts {
			mergeCrawlOutcomeCount(&monitor.Totals.OutcomeCounts, outcome)
		}
	}
	sort.Slice(monitor.Totals.OutcomeCounts, func(left, right int) bool {
		if monitor.Totals.OutcomeCounts[left].Class == monitor.Totals.OutcomeCounts[right].Class {
			return monitor.Totals.OutcomeCounts[left].Reason < monitor.Totals.OutcomeCounts[right].Reason
		}
		return monitor.Totals.OutcomeCounts[left].Class < monitor.Totals.OutcomeCounts[right].Class
	})
	return monitor
}

func mergeCrawlOutcomeCount(target *[]adminui.CrawlOutcomeCount, source yagocrawlcontract.CrawlOutcomeCount) {
	for index := range *target {
		count := &(*target)[index]
		if count.Class == source.Class && count.Reason == source.Reason {
			count.Pages = crawlTallyTotal(count.Pages, source.Pages)
			return
		}
	}
	*target = append(*target, adminui.CrawlOutcomeCount{
		Class: source.Class, Reason: source.Reason, Pages: source.Pages,
	})
}

func crawlRunView(run crawlruns.Run, now time.Time) adminui.CrawlRunView {
	state := run.State
	pending := run.Tally.Pending
	if state == yagocrawlcontract.CrawlRunRunning &&
		(now.Before(run.Updated) || now.Sub(run.Updated) >
			3*yagocrawlcontract.DefaultWorkerHeartbeatInterval) {
		state = "stale"
		pending = 0
	}
	end := run.Updated
	if state == yagocrawlcontract.CrawlRunRunning {
		end = now
	}
	runtime := end.Sub(run.FirstSeen)
	if runtime < 0 {
		runtime = 0
	}
	reportAge := "Unavailable"
	if !run.Updated.IsZero() {
		age := now.Sub(run.Updated)
		if age < 0 {
			age = 0
		}
		reportAge = age.Round(time.Second).String() + " ago"
	}

	return adminui.CrawlRunView{
		RunID:           run.RunID,
		Profile:         crawlRunLabel(run),
		Worker:          run.WorkerID,
		State:           string(state),
		Fetched:         run.Tally.Fetched,
		Indexed:         run.Tally.Indexed,
		Failed:          run.Tally.Failed,
		RobotsDenied:    run.Tally.RobotsDenied,
		Duplicates:      run.Tally.Duplicates,
		Pending:         pending,
		Runtime:         runtime.Round(time.Second).String(),
		PagesPerMinute:  run.PagesPerMinute,
		RateKnown:       run.RateKnown,
		MaxPagesPerHost: crawlRunPerHostLimit(run),
		MaxPagesPerRun:  crawlRunWholeLimit(run),
		ReportAge:       reportAge,
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
