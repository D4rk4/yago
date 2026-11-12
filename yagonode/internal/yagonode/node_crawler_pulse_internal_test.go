package yagonode

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/crawlruns"
)

type crawlerWorkerActivityFixture struct {
	workers []crawlbroker.CrawlerWorkerSnapshot
	runtime crawlbroker.CrawlerRuntimeSnapshot
}

func (f crawlerWorkerActivityFixture) WorkerSnapshots() []crawlbroker.CrawlerWorkerSnapshot {
	return f.workers
}

func (f crawlerWorkerActivityFixture) RuntimeSnapshot() crawlbroker.CrawlerRuntimeSnapshot {
	return f.runtime
}

type crawlerRunThroughputFixture struct {
	workers []crawlruns.WorkerThroughput
	runs    []crawlruns.Run
}

func (f crawlerRunThroughputFixture) Throughput(time.Duration) []crawlruns.WorkerThroughput {
	return f.workers
}

func (f crawlerRunThroughputFixture) Recent() []crawlruns.Run {
	return f.runs
}

func TestCrawlerProcessLabelBoundsIdentifierDisplay(t *testing.T) {
	if got := crawlerProcessLabel("a1b2c3d4e5"); got != "a1b2c3d4" {
		t.Fatalf("long session label = %q, want eight-character process label", got)
	}
	if got := crawlerProcessLabel("short"); got != "short" {
		t.Fatalf("short session label = %q, want unmodified value", got)
	}
}

func TestCrawlerPulseSourceCombinesCurrentProcessRatesAndQueueDepth(t *testing.T) {
	now := time.Now()
	source := newCrawlerPulseSource(
		crawlerWorkerActivityFixture{
			workers: []crawlbroker.CrawlerWorkerSnapshot{{
				WorkerID:                   "worker-a",
				ProcessIdentifier:          "a1b2c3d4e5f6",
				ActiveFetches:              3,
				ActiveFetchesKnown:         true,
				FetchAdmissionWaiting:      1,
				FetchAdmissionWaitingKnown: true,
				ActivePages: []crawlbroker.CrawlerActivePage{{
					URL:                 "https://example.org/page",
					Stage:               "node_ingest",
					ElapsedMilliseconds: 6200,
				}},
				ObservedAt: now.Add(-5 * time.Second),
			}},
			runtime: crawlbroker.CrawlerRuntimeSnapshot{FetchLimitPerCrawler: 8},
		},
		crawlerRunThroughputFixture{
			workers: []crawlruns.WorkerThroughput{{
				WorkerID:              "worker-a",
				ProcessIdentifier:     "a1b2c3d4e5f6",
				FetchedPagesPerMinute: 24.5,
				IndexedPagesPerMinute: 20,
				RateKnown:             true,
				ObservedAt:            now,
			}},
			runs: []crawlruns.Run{{
				State:   yagocrawlcontract.CrawlRunRunning,
				Tally:   yagocrawlcontract.CrawlRunTally{Pending: 4},
				Updated: now,
			}},
		},
	)
	pulse := source.CrawlerPulse(context.Background())
	if !pulse.Available || !pulse.QueueKnown || pulse.QueuePending != 4 || len(pulse.Workers) != 1 {
		t.Fatalf("crawler pulse = %+v, want one process and a known queue depth", pulse)
	}
	worker := pulse.Workers[0]
	if worker.WorkerID != "worker-a" || worker.ProcessLabel != "a1b2c3d4" ||
		worker.ActiveFetches != 3 || worker.FetchAdmissionWaiting != 1 || worker.FetchLimit != 8 ||
		!worker.RateKnown || worker.FetchedPagesPerMinute != 24.5 ||
		worker.IndexedPagesPerMinute != 20 || !worker.ObservedAt.Equal(now) ||
		len(worker.ActivePages) != 1 || worker.ActivePages[0].URL != "https://example.org/page" ||
		worker.ActivePages[0].Stage != "node_ingest" || worker.ActivePages[0].ElapsedMilliseconds != 6200 {
		t.Fatalf("crawler process pulse = %+v, want current measured process rates", worker)
	}
}

func TestCrawlerPulseSourceDoesNotApplyOldProcessRatesOrFailedQueueProbe(t *testing.T) {
	source := crawlerPulseSource{
		control: crawlerWorkerActivityFixture{
			workers: []crawlbroker.CrawlerWorkerSnapshot{{
				WorkerID:          "worker-a",
				ProcessIdentifier: "process-current",
			}},
			runtime: crawlbroker.CrawlerRuntimeSnapshot{FetchLimitPerCrawler: 8},
		},
		runs: crawlerRunThroughputFixture{workers: []crawlruns.WorkerThroughput{{
			WorkerID:              "worker-a",
			ProcessIdentifier:     "process-old",
			FetchedPagesPerMinute: 100,
			IndexedPagesPerMinute: 90,
			RateKnown:             true,
		}}},
	}
	pulse := source.CrawlerPulse(context.Background())
	if pulse.QueueKnown || pulse.QueuePending != 0 || len(pulse.Workers) != 1 {
		t.Fatalf("failed queue pulse = %+v, want unavailable queue and one worker", pulse)
	}
	worker := pulse.Workers[0]
	if worker.ProcessLabel != "process-" || worker.RateKnown || worker.FetchedPagesPerMinute != 0 {
		t.Fatalf("current process used stale rates = %+v", worker)
	}

}
