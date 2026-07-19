package yagonode

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/crawlruns"
)

var errMonitorProbe = errors.New("probe failed")

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
		Tally:       yagocrawlcontract.CrawlRunTally{Fetched: 10, Indexed: 6, Duplicates: 2},
	})
	source := newCrawlMonitorSource(reg, func(context.Context) (crawlbroker.QueueDepth, error) {
		return crawlbroker.QueueDepth{Pending: 3, Leased: 1}, nil
	})

	monitor := source.Monitor(context.Background())
	if len(monitor.Runs) != 2 {
		t.Fatalf("runs = %d, want 2", len(monitor.Runs))
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
	if !monitor.QueueAvailable || monitor.QueuePending != 3 || monitor.QueueLeased != 1 {
		t.Fatalf("queue = %d/%d, want 3/1", monitor.QueuePending, monitor.QueueLeased)
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
}

func TestCrawlMonitorSourceProbeErrorIsUnavailable(t *testing.T) {
	reg := crawlruns.New(8)
	recordRun(reg, yagocrawlcontract.CrawlRunProgress{
		RunID: "r1",
		State: yagocrawlcontract.CrawlRunRunning,
	})
	source := newCrawlMonitorSource(reg, func(context.Context) (crawlbroker.QueueDepth, error) {
		return crawlbroker.QueueDepth{}, errMonitorProbe
	})

	monitor := source.Monitor(context.Background())
	if len(monitor.Runs) != 1 {
		t.Fatalf("runs = %d, want 1 despite probe error", len(monitor.Runs))
	}
	if monitor.QueueAvailable {
		t.Fatal("failed queue probe should be unavailable")
	}
}

func TestCrawlMonitorSourceNilProbe(t *testing.T) {
	source := newCrawlMonitorSource(crawlruns.New(8), nil)

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
	source := newCrawlMonitorSource(reg, nil)
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
			RobotsDenied: math.MaxUint64, Duplicates: math.MaxUint64,
		},
	})
	recordRun(reg, yagocrawlcontract.CrawlRunProgress{
		RunID: "one", State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{
			Fetched: 1, Indexed: 1, Failed: 1, RobotsDenied: 1, Duplicates: 1,
		},
	})

	totals := newCrawlMonitorSource(reg, nil).Monitor(t.Context()).Totals
	if totals.Fetched != math.MaxUint64 || totals.Indexed != math.MaxUint64 ||
		totals.Failed != math.MaxUint64 || totals.RobotsDenied != math.MaxUint64 ||
		totals.Duplicates != math.MaxUint64 {
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
