package crawlruns

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestThroughputMeasuresFetchedAndIndexedPagesSeparately(t *testing.T) {
	registry := New(8)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	registry.now = func() time.Time { return now }
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-a", WorkerID: "worker-a", State: yagocrawlcontract.CrawlRunRunning,
		PagesPerMinute: 900, RateKnown: true,
	})
	if got := registry.Throughput(time.Minute); len(got) != 1 || got[0].RateKnown {
		t.Fatalf("throughput after one sample = %+v, want an unknown worker rate", got)
	}
	now = now.Add(30 * time.Second)
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-a", WorkerID: "worker-a", State: yagocrawlcontract.CrawlRunRunning,
		Tally:          yagocrawlcontract.CrawlRunTally{Fetched: 20, Indexed: 12},
		PagesPerMinute: 900, RateKnown: true,
	})
	got := registry.Throughput(time.Minute)
	if len(got) != 1 || !got[0].RateKnown {
		t.Fatalf("measured throughput = %+v, want a known worker rate", got)
	}
	if math.Abs(got[0].FetchedPagesPerMinute-40) > 0.001 ||
		math.Abs(got[0].IndexedPagesPerMinute-24) > 0.001 {
		t.Fatalf(
			"measured fetched/indexed rates = %.2f/%.2f, want 40/24",
			got[0].FetchedPagesPerMinute,
			got[0].IndexedPagesPerMinute,
		)
	}
}

func TestThroughputUsesDefaultWindowForNonpositiveDurations(t *testing.T) {
	registry := New(4)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	registry.now = func() time.Time { return now }
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-window", WorkerID: "worker-window", State: yagocrawlcontract.CrawlRunRunning,
	})
	now = now.Add(30 * time.Second)
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-window", WorkerID: "worker-window", State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 12, Indexed: 6},
	})
	defaultWindow := registry.Throughput(defaultThroughputWindow)
	for _, window := range []time.Duration{0, -time.Second} {
		got := registry.Throughput(window)
		if len(got) != 1 || !got[0].RateKnown ||
			got[0].FetchedPagesPerMinute != defaultWindow[0].FetchedPagesPerMinute ||
			got[0].IndexedPagesPerMinute != defaultWindow[0].IndexedPagesPerMinute {
			t.Fatalf(
				"throughput for window %s = %+v, want default-window rate %+v",
				window,
				got,
				defaultWindow,
			)
		}
	}
}

func TestThroughputAggregatesCurrentRunsAndDropsStaleOrTerminalRuns(t *testing.T) {
	registry := New(8)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	registry.now = func() time.Time { return now }
	for _, runID := range []string{"run-a", "run-b"} {
		registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
			RunID: runID, WorkerID: "worker-a", State: yagocrawlcontract.CrawlRunRunning,
		})
	}
	now = now.Add(20 * time.Second)
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-a", WorkerID: "worker-a", State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 10, Indexed: 8},
	})
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-b", WorkerID: "worker-a", State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 5, Indexed: 4},
	})
	got := registry.Throughput(time.Minute)
	if len(got) != 1 || !got[0].RateKnown ||
		math.Abs(got[0].FetchedPagesPerMinute-45) > 0.001 ||
		math.Abs(got[0].IndexedPagesPerMinute-36) > 0.001 {
		t.Fatalf("aggregated worker rate = %+v, want fetched/indexed 45/36", got)
	}
	now = now.Add(progressFreshness + time.Nanosecond)
	if got := registry.Throughput(time.Minute); len(got) != 0 {
		t.Fatalf("stale worker throughput = %+v, want no worker rate", got)
	}
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-a", WorkerID: "worker-a", State: yagocrawlcontract.CrawlRunFinished,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 10, Indexed: 8},
	})
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-b", WorkerID: "worker-a", State: yagocrawlcontract.CrawlRunFinished,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 5, Indexed: 4},
	})
	if got := registry.Throughput(time.Minute); len(got) != 0 {
		t.Fatalf("terminal worker throughput = %+v, want no active rate", got)
	}
}

func TestThroughputWaitsForEveryRunInAWorkerProcess(t *testing.T) {
	registry := New(8)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	registry.now = func() time.Time { return now }
	for _, runID := range []string{"run-a", "run-b"} {
		registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
			RunID: runID, WorkerID: "worker-a", ProcessIdentifier: "session-a",
			State: yagocrawlcontract.CrawlRunRunning,
		})
	}
	now = now.Add(10 * time.Second)
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-a", WorkerID: "worker-a", ProcessIdentifier: "session-a",
		State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 10, Indexed: 5},
	})
	if got := registry.Throughput(time.Minute); len(got) != 1 || got[0].RateKnown {
		t.Fatalf("partial process throughput = %+v, want unknown until both runs report", got)
	}
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-b", WorkerID: "worker-a", ProcessIdentifier: "session-a",
		State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 15, Indexed: 9},
	})
	got := registry.Throughput(time.Minute)
	if len(got) != 1 || !got[0].RateKnown ||
		got[0].FetchedPagesPerMinute != 150 || got[0].IndexedPagesPerMinute != 84 {
		t.Fatalf("complete process throughput = %+v, want fetched/indexed 150/84 per minute", got)
	}
}

func TestThroughputKeepsCrawlerProcessesSeparate(t *testing.T) {
	registry := New(8)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	registry.now = func() time.Time { return now }
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-old", WorkerID: "worker-a", ProcessIdentifier: "session-old",
		State: yagocrawlcontract.CrawlRunRunning,
	})
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-current", WorkerID: "worker-a", ProcessIdentifier: "session-current",
		State: yagocrawlcontract.CrawlRunRunning,
	})
	now = now.Add(30 * time.Second)
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-old", WorkerID: "worker-a", ProcessIdentifier: "session-old",
		State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 6, Indexed: 3},
	})
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-current", WorkerID: "worker-a", ProcessIdentifier: "session-current",
		State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 20, Indexed: 12},
	})
	got := registry.Throughput(time.Minute)
	if len(got) != 2 {
		t.Fatalf("process throughput = %+v, want separate rows for both sessions", got)
	}
	if got[0].ProcessIdentifier != "session-current" || !got[0].RateKnown ||
		got[0].FetchedPagesPerMinute != 40 || got[0].IndexedPagesPerMinute != 24 {
		t.Fatalf("current process throughput = %+v, want 40 fetched and 24 indexed/minute", got[0])
	}
	if got[1].ProcessIdentifier != "session-old" || !got[1].RateKnown ||
		got[1].FetchedPagesPerMinute != 12 || got[1].IndexedPagesPerMinute != 6 {
		t.Fatalf("previous process throughput = %+v, want 12 fetched and 6 indexed/minute", got[1])
	}
}

func TestThroughputStartsFreshWhenRunMovesToNewCrawlerProcess(t *testing.T) {
	registry := New(4)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	registry.now = func() time.Time { return now }
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-recovered", WorkerID: "worker-a", ProcessIdentifier: "session-old",
		State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 100, Indexed: 80},
	})
	now = now.Add(30 * time.Second)
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-recovered", WorkerID: "worker-a", ProcessIdentifier: "session-current",
		State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 1000, Indexed: 900},
	})
	if got := registry.Throughput(time.Minute); len(got) != 1 || got[0].RateKnown {
		t.Fatalf("throughput after session replacement = %+v, want fresh unknown rate", got)
	}
	now = now.Add(30 * time.Second)
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-recovered", WorkerID: "worker-a", ProcessIdentifier: "session-current",
		State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 1010, Indexed: 906},
	})
	got := registry.Throughput(time.Minute)
	if len(got) != 1 || got[0].ProcessIdentifier != "session-current" || !got[0].RateKnown ||
		got[0].FetchedPagesPerMinute != 20 || got[0].IndexedPagesPerMinute != 12 {
		t.Fatalf(
			"throughput after current session samples = %+v, want 20 fetched and 12 indexed/minute",
			got,
		)
	}
}

func TestThroughputResetsAfterCounterRegression(t *testing.T) {
	registry := New(2)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	registry.now = func() time.Time { return now }
	for _, fetched := range []uint64{10, 2} {
		registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
			RunID: "run-reset", WorkerID: "worker-reset", State: yagocrawlcontract.CrawlRunRunning,
			Tally: yagocrawlcontract.CrawlRunTally{Fetched: fetched, Indexed: fetched},
		})
		now = now.Add(10 * time.Second)
	}
	if got := registry.Throughput(time.Minute); len(got) != 1 || got[0].RateKnown {
		t.Fatalf("throughput after reset = %+v, want unknown", got)
	}
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-reset", WorkerID: "worker-reset", State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 7, Indexed: 5},
	})
	got := registry.Throughput(time.Minute)
	if len(got) != 1 || !got[0].RateKnown || got[0].FetchedPagesPerMinute <= 0 {
		t.Fatalf("recovered throughput = %+v, want a fresh measured rate", got)
	}
}

func TestThroughputReplacesSameTimeProgressSample(t *testing.T) {
	registry := New(4)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	registry.now = func() time.Time { return now }
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-same-time", WorkerID: "worker-a", State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 1, Indexed: 1},
	})
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-same-time", WorkerID: "worker-a", State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 5, Indexed: 2},
	})
	now = now.Add(30 * time.Second)
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-same-time", WorkerID: "worker-a", State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 15, Indexed: 8},
	})
	got := registry.Throughput(time.Minute)
	if len(got) != 1 || !got[0].RateKnown || got[0].FetchedPagesPerMinute != 20 ||
		got[0].IndexedPagesPerMinute != 12 {
		t.Fatalf("same-time throughput = %+v, want 20 fetched and 12 indexed/minute", got)
	}
}

func TestThroughputBoundsSamplesAndUsesTheWindowStart(t *testing.T) {
	registry := New(4)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	registry.now = func() time.Time { return now }
	for sample := range maximumThroughputSamples + 1 {
		registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
			RunID: "run-bounded", WorkerID: "worker-a", State: yagocrawlcontract.CrawlRunRunning,
			Tally: yagocrawlcontract.CrawlRunTally{
				Fetched: uint64(sample), Indexed: uint64(sample),
			},
		})
		now = now.Add(5 * time.Second)
	}
	got := registry.Throughput(time.Minute)
	if len(got) != 1 || !got[0].RateKnown || got[0].FetchedPagesPerMinute != 12 ||
		got[0].IndexedPagesPerMinute != 12 {
		t.Fatalf("bounded sample throughput = %+v, want 12 pages/minute", got)
	}
}

func TestThroughputSortsDifferentWorkers(t *testing.T) {
	registry := New(4)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	registry.now = func() time.Time { return now }
	for _, workerID := range []string{"worker-b", "worker-a"} {
		registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
			RunID: workerID, WorkerID: workerID, State: yagocrawlcontract.CrawlRunRunning,
		})
	}
	now = now.Add(30 * time.Second)
	for _, workerID := range []string{"worker-b", "worker-a"} {
		registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
			RunID: workerID, WorkerID: workerID, State: yagocrawlcontract.CrawlRunRunning,
		})
	}
	got := registry.Throughput(time.Minute)
	if len(got) != 2 || got[0].WorkerID != "worker-a" || got[1].WorkerID != "worker-b" {
		t.Fatalf("throughput worker order = %+v, want worker-a then worker-b", got)
	}
}

func TestThroughputRejectsNegativeElapsedSampleWindow(t *testing.T) {
	registry := New(4)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	registry.now = func() time.Time { return now }
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-clock", WorkerID: "worker-clock", State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 1, Indexed: 1},
	})
	now = now.Add(-10 * time.Second)
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-clock", WorkerID: "worker-clock", State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 2, Indexed: 2},
	})
	got := registry.Throughput(time.Minute)
	if len(got) != 1 || got[0].RateKnown {
		t.Fatalf("throughput after clock regression = %+v, want unknown rate", got)
	}
}

func TestThroughputExcludesFutureProgressAfterClockMovesBackward(t *testing.T) {
	registry := New(4)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	registry.now = func() time.Time { return now }
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-future", WorkerID: "worker-future", State: yagocrawlcontract.CrawlRunRunning,
	})
	now = now.Add(-time.Second)
	if got := registry.Throughput(time.Minute); len(got) != 0 {
		t.Fatalf("throughput after clock rollback = %+v, want future report omitted", got)
	}
}
