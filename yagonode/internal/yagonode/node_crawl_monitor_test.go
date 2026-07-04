package yagonode

import (
	"context"
	"errors"
	"testing"

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
	if monitor.QueuePending != 3 || monitor.QueueLeased != 1 {
		t.Fatalf("queue = %d/%d, want 3/1", monitor.QueuePending, monitor.QueueLeased)
	}
}

func TestCrawlMonitorSourceProbeErrorDegrades(t *testing.T) {
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
	if monitor.QueuePending != 0 || monitor.QueueLeased != 0 {
		t.Fatalf(
			"queue = %d/%d, want 0/0 on probe error",
			monitor.QueuePending,
			monitor.QueueLeased,
		)
	}
}

func TestCrawlMonitorSourceNilProbe(t *testing.T) {
	source := newCrawlMonitorSource(crawlruns.New(8), nil)

	monitor := source.Monitor(context.Background())
	if monitor.QueuePending != 0 || len(monitor.Runs) != 0 {
		t.Fatalf("nil-probe monitor = %+v, want empty", monitor)
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
