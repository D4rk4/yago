package yagonode

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawlruns"
)

func recordRun(reg *crawlruns.Registry, progress yagocrawlcontract.CrawlRunProgress) {
	reg.Record(context.Background(), progress)
}

func monitorRunByProfile(monitor adminui.CrawlMonitor, profile string) adminui.CrawlRunView {
	for _, run := range monitor.Runs {
		if run.Profile == profile {
			return run
		}
	}

	return adminui.CrawlRunView{}
}

func TestCrawlMonitorSourceSnapshot(t *testing.T) {
	reg := crawlruns.New(8)
	recordRun(reg, yagocrawlcontract.CrawlRunProgress{
		RunID:       "r1",
		ProfileName: "news",
		WorkerID:    "w1",
		State:       yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{
			Fetched: 5, Indexed: 4, Failed: 1, RobotsDenied: 2, Duplicates: 3, Pending: 6,
		},
		PagesPerMinute:  30,
		RateKnown:       true,
		MaxPagesPerHost: 250,
		MaxPagesPerRun:  900,
		LimitsKnown:     true,
	})
	recordRun(reg, yagocrawlcontract.CrawlRunProgress{
		RunID:       "r2",
		ProfileName: "docs",
		State:       yagocrawlcontract.CrawlRunFinished,
		Tally: yagocrawlcontract.CrawlRunTally{
			Fetched:    10,
			Indexed:    6,
			Duplicates: 2,
			Pending:    100,
		},
	})
	recordRun(reg, yagocrawlcontract.CrawlRunProgress{
		RunID:       "r3",
		ProfileName: "manual",
		State:       yagocrawlcontract.CrawlRunRunning,
		Tally:       yagocrawlcontract.CrawlRunTally{Pending: 7},
	})
	source := newCrawlMonitorSource(reg)

	monitor := source.Monitor(context.Background())
	if len(monitor.Runs) != 3 {
		t.Fatalf("runs = %d, want 3", len(monitor.Runs))
	}
	if monitor.Totals.Fetched != 15 || monitor.Totals.Indexed != 10 ||
		monitor.Totals.Failed != 1 || monitor.Totals.RobotsDenied != 2 ||
		monitor.Totals.Duplicates != 5 {
		t.Fatalf("totals = %+v, want summed across both runs", monitor.Totals)
	}
	run := monitorRunByProfile(monitor, "news")
	if run.Profile != "news" || run.Worker != "w1" || run.State != "running" {
		t.Fatalf("run view identity = %+v", run)
	}
	if run.Fetched != 5 || run.Indexed != 4 || run.Failed != 1 ||
		run.RobotsDenied != 2 || run.Duplicates != 3 || run.Pending != 6 {
		t.Fatalf("run view tally = %+v", run)
	}
	if run.PagesPerMinute != 30 || !run.RateKnown {
		t.Fatalf("run view rate = %d/%v, want known 30", run.PagesPerMinute, run.RateKnown)
	}
	if run.MaxPagesPerHost != "250" || run.MaxPagesPerRun != "900" {
		t.Fatalf("run limits = %q/%q", run.MaxPagesPerHost, run.MaxPagesPerRun)
	}
	if monitor.QueueAvailable {
		t.Fatal("node-owned crawl-order depth must not be reported")
	}
	if monitor.PendingFrontierURLs != 13 {
		t.Fatalf(
			"pending frontier URLs = %d, want 13 from running runs only",
			monitor.PendingFrontierURLs,
		)
	}
}

func TestCrawlMonitorSeparatesNewOutcomesFromLegacyUnknownPages(t *testing.T) {
	reg := crawlruns.New(8)
	recordRun(reg, yagocrawlcontract.CrawlRunProgress{
		RunID: "legacy", State: yagocrawlcontract.CrawlRunFinished,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 103321, Indexed: 16237},
	})
	recordRun(reg, yagocrawlcontract.CrawlRunProgress{
		RunID: "classified", State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{
			Fetched: 10, Indexed: 5, DocumentCandidates: 7, RemovalMutations: 1,
			NotIndexable: 2, NodeRejected: 2, PendingIngest: 1,
			OutcomeCounts: []yagocrawlcontract.CrawlOutcomeCount{
				{Class: "not_indexable", Reason: "sitemap", Pages: 2},
				{Class: "node_rejected", Reason: "too-few-words", Pages: 2},
			},
		},
	})

	monitor := newCrawlMonitorSource(reg).Monitor(context.Background())
	if monitor.Totals.UnknownOutcome != 87084 || monitor.Totals.PendingIngest != 1 ||
		monitor.Totals.NotIndexable != 2 || monitor.Totals.NodeRejected != 2 ||
		monitor.Totals.DocumentCandidates != 7 || monitor.Totals.RemovalMutations != 1 {
		t.Fatalf("classified totals = %+v", monitor.Totals)
	}
	if len(monitor.Totals.OutcomeCounts) != 2 ||
		monitor.Totals.OutcomeCounts[0].Class != "node_rejected" ||
		monitor.Totals.OutcomeCounts[0].Reason != "too-few-words" ||
		monitor.Totals.OutcomeCounts[1].Class != "not_indexable" ||
		monitor.Totals.OutcomeCounts[1].Reason != "sitemap" {
		t.Fatalf("outcome reasons = %+v", monitor.Totals.OutcomeCounts)
	}
}

func TestCrawlRunViewFormatsUnlimitedAndUnavailableLimits(t *testing.T) {
	unlimited := crawlRunView(crawlruns.Run{
		LimitsKnown: true, MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	}, time.Now())
	if unlimited.MaxPagesPerHost != "Unlimited" || unlimited.MaxPagesPerRun != "Unlimited" {
		t.Fatalf("unlimited limits = %q/%q", unlimited.MaxPagesPerHost, unlimited.MaxPagesPerRun)
	}
	unknown := crawlRunView(crawlruns.Run{}, time.Now())
	if unknown.MaxPagesPerHost != "Unavailable" || unknown.MaxPagesPerRun != "Unavailable" {
		t.Fatalf("unknown limits = %q/%q", unknown.MaxPagesPerHost, unknown.MaxPagesPerRun)
	}
	if unknown.ReportAge != "Unavailable" {
		t.Fatalf("unknown report age = %q, want Unavailable", unknown.ReportAge)
	}

	now := time.Now()
	recent := crawlRunView(crawlruns.Run{Updated: now.Add(-15 * time.Second)}, now)
	if recent.ReportAge != "15s ago" {
		t.Fatalf("report age = %q, want 15s ago", recent.ReportAge)
	}
}

func TestCrawlMonitorSourceHasNoNodeOrderQueue(t *testing.T) {
	source := newCrawlMonitorSource(crawlruns.New(8))

	monitor := source.Monitor(context.Background())
	if monitor.QueueAvailable || monitor.QueuePending != 0 || len(monitor.Runs) != 0 {
		t.Fatalf("nil-probe monitor = %+v, want empty", monitor)
	}
}

func TestCrawlMonitorRuntimeUsesCurrentTimeOnlyForActiveRuns(t *testing.T) {
	reg := crawlruns.New(8)
	recordRun(reg, yagocrawlcontract.CrawlRunProgress{
		RunID: "active", ProfileName: "active", State: yagocrawlcontract.CrawlRunRunning,
	})
	activeStart := reg.Recent()[0].FirstSeen
	recordRun(reg, yagocrawlcontract.CrawlRunProgress{
		RunID: "finished", ProfileName: "finished", State: yagocrawlcontract.CrawlRunRunning,
	})
	recordRun(reg, yagocrawlcontract.CrawlRunProgress{
		RunID: "finished", ProfileName: "finished", State: yagocrawlcontract.CrawlRunFinished,
	})
	finished := reg.Recent()[0]
	source := newCrawlMonitorSource(reg)
	source.now = func() time.Time { return activeStart.Add(2 * time.Hour) }

	monitor := source.Monitor(t.Context())
	if got := monitorRunByProfile(monitor, "active").Runtime; got != "2h0m0s" {
		t.Fatalf("active runtime = %q, want 2h0m0s", got)
	}
	wantFinished := finished.Updated.Sub(finished.FirstSeen).Round(time.Second).String()
	if got := monitorRunByProfile(monitor, "finished").Runtime; got != wantFinished {
		t.Fatalf("finished runtime = %q, want %q", got, wantFinished)
	}

	source.now = func() time.Time { return activeStart.Add(-time.Hour) }
	if got := monitorRunByProfile(source.Monitor(t.Context()), "active").Runtime; got != "0s" {
		t.Fatalf("negative runtime = %q, want 0s", got)
	}
}

func TestCrawlMonitorTotalsSaturateInsteadOfWrapping(t *testing.T) {
	reg := crawlruns.New(8)
	recordRun(reg, yagocrawlcontract.CrawlRunProgress{
		RunID: "large", State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{
			Fetched: math.MaxUint64, Indexed: math.MaxUint64, Failed: math.MaxUint64,
			RobotsDenied: math.MaxUint64, Duplicates: math.MaxUint64, Pending: math.MaxUint64,
		},
	})
	recordRun(reg, yagocrawlcontract.CrawlRunProgress{
		RunID: "one", State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{
			Fetched: 1, Indexed: 1, Failed: 1, RobotsDenied: 1, Duplicates: 1, Pending: 1,
		},
	})

	totals := newCrawlMonitorSource(reg).Monitor(t.Context()).Totals
	if totals.Fetched != math.MaxUint64 || totals.Indexed != math.MaxUint64 ||
		totals.Failed != math.MaxUint64 || totals.RobotsDenied != math.MaxUint64 ||
		totals.Duplicates != math.MaxUint64 ||
		newCrawlMonitorSource(reg).Monitor(t.Context()).PendingFrontierURLs != math.MaxUint64 {
		t.Fatalf("saturated totals = %+v", totals)
	}
}

func TestCrawlRunRegistryLiveAndBare(t *testing.T) {
	if crawlRunRegistry(bareCrawlProcess{}) != nil {
		t.Fatal("bare crawl process should expose no run registry")
	}
	if crawlRunRegistry(liveCrawlRuntime(t)) == nil {
		t.Fatal("live crawl runtime should expose a run registry")
	}
}
