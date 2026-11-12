package crawlruns

import (
	"context"
	"crypto/sha256"
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

func TestShortThroughputWindowDoesNotDiscardMinuteSamples(t *testing.T) {
	registry := New(4)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	registry.now = func() time.Time { return now }
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID:    "run-retained",
		WorkerID: "worker-retained",
		State:    yagocrawlcontract.CrawlRunRunning,
	})
	now = now.Add(30 * time.Second)
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID:    "run-retained",
		WorkerID: "worker-retained",
		State:    yagocrawlcontract.CrawlRunRunning,
		Tally:    yagocrawlcontract.CrawlRunTally{Fetched: 2, Indexed: 1},
	})
	now = now.Add(5 * time.Second)
	if got := registry.Throughput(time.Second); len(got) != 0 {
		t.Fatalf("short-window throughput = %+v, want no rate outside its window", got)
	}
	got := registry.Throughput(time.Minute)
	if len(got) != 1 || !got[0].RateKnown ||
		got[0].FetchedPagesPerMinute != 4 || got[0].IndexedPagesPerMinute != 2 {
		t.Fatalf(
			"minute throughput after short query = %+v, want 4 fetched and 2 indexed/minute",
			got,
		)
	}
}

func TestThroughputRemovesProcessRatesAfterRetention(t *testing.T) {
	registry := New(4)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	registry.now = func() time.Time { return now }
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-expiring", WorkerID: "worker-expiring",
		State: yagocrawlcontract.CrawlRunRunning,
	})
	now = now.Add(10 * time.Second)
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-expiring", WorkerID: "worker-expiring",
		State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 2, Indexed: 1},
	})
	identity := crawlerProcessIdentity{workerID: "worker-expiring"}
	if _, found := registry.processRates[identity]; !found {
		t.Fatal("expected active process throughput to be retained")
	}
	now = now.Add(defaultThroughputWindow + time.Second)
	if got := registry.Throughput(defaultThroughputWindow); len(got) != 0 {
		t.Fatalf("stale process throughput = %+v, want no retained workers", got)
	}
	if _, found := registry.processRates[identity]; found {
		t.Fatal("expected stale process throughput to be removed")
	}
}

func TestThroughputAggregatesRunDeltasAndRetainsRecentTerminalRates(t *testing.T) {
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
	now = now.Add(20 * time.Second)
	if got := registry.Throughput(time.Minute); len(got) != 1 || !got[0].RateKnown {
		t.Fatalf("recent worker throughput = %+v, want the measured window retained", got)
	}
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-a", WorkerID: "worker-a", State: yagocrawlcontract.CrawlRunFinished,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 10, Indexed: 8},
	})
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-b", WorkerID: "worker-a", State: yagocrawlcontract.CrawlRunFinished,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 5, Indexed: 4},
	})
	if got := registry.Throughput(time.Minute); len(got) != 1 || !got[0].RateKnown {
		t.Fatalf("terminal worker throughput = %+v, want its recent process rate", got)
	}
}

func TestThroughputCountsPagesFromRunsThatFinishBeforeTheNextRun(t *testing.T) {
	registry := New(8)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	registry.now = func() time.Time { return now }
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "short-run-a", WorkerID: "worker-a", ProcessIdentifier: "process-a",
		State: yagocrawlcontract.CrawlRunRunning,
	})
	now = now.Add(2 * time.Second)
	recordTerminalThroughput(t, registry, "terminal-a", yagocrawlcontract.CrawlRunProgress{
		RunID: "short-run-a", WorkerID: "worker-a", ProcessIdentifier: "process-a",
		State: yagocrawlcontract.CrawlRunFinished,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 2, Indexed: 1},
	})
	now = now.Add(time.Second)
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "short-run-b", WorkerID: "worker-a", ProcessIdentifier: "process-a",
		State: yagocrawlcontract.CrawlRunRunning,
	})
	now = now.Add(time.Second)
	recordTerminalThroughput(t, registry, "terminal-b", yagocrawlcontract.CrawlRunProgress{
		RunID: "short-run-b", WorkerID: "worker-a", ProcessIdentifier: "process-a",
		State: yagocrawlcontract.CrawlRunFinished,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 3, Indexed: 2},
	})
	got := registry.Throughput(time.Minute)
	if len(got) != 1 || !got[0].RateKnown ||
		math.Abs(got[0].FetchedPagesPerMinute-75) > 0.001 ||
		math.Abs(got[0].IndexedPagesPerMinute-45) > 0.001 {
		t.Fatalf("short-run throughput = %+v, want 75 fetched and 45 indexed/minute", got)
	}
}

func TestThroughputCountsTerminalRunsFirstSeenAtSettlement(t *testing.T) {
	registry := New(8)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	registry.now = func() time.Time { return now }
	recordTerminalThroughput(t, registry, "terminal-only-a", yagocrawlcontract.CrawlRunProgress{
		RunID: "terminal-only-run-a", WorkerID: "worker-a", ProcessIdentifier: "process-a",
		State: yagocrawlcontract.CrawlRunFinished,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 2, Indexed: 1},
	})
	now = now.Add(30 * time.Second)
	recordTerminalThroughput(t, registry, "terminal-only-b", yagocrawlcontract.CrawlRunProgress{
		RunID: "terminal-only-run-b", WorkerID: "worker-a", ProcessIdentifier: "process-a",
		State: yagocrawlcontract.CrawlRunFinished,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 3, Indexed: 2},
	})
	got := registry.Throughput(time.Minute)
	if len(got) != 1 || !got[0].RateKnown ||
		got[0].FetchedPagesPerMinute != 6 || got[0].IndexedPagesPerMinute != 4 {
		t.Fatalf("terminal-only throughput = %+v, want 6 fetched and 4 indexed/minute", got)
	}
}

func recordTerminalThroughput(
	t *testing.T,
	registry *Registry,
	identity string,
	progress yagocrawlcontract.CrawlRunProgress,
) {
	t.Helper()
	key := sha256.Sum256([]byte(identity))
	if err := registry.RecordTerminal(t.Context(), key[:], progress); err != nil {
		t.Fatalf("record terminal progress: %v", err)
	}
	if err := registry.ConfirmTerminalDelivery(t.Context(), key[:]); err != nil {
		t.Fatalf("confirm terminal progress: %v", err)
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
	got := registry.Throughput(time.Minute)
	if len(got) != 2 || throughputForProcess(got, "session-current").RateKnown {
		t.Fatalf(
			"throughput after session replacement = %+v, want current process rate unknown",
			got,
		)
	}
	now = now.Add(30 * time.Second)
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-recovered", WorkerID: "worker-a", ProcessIdentifier: "session-current",
		State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 1010, Indexed: 906},
	})
	got = registry.Throughput(time.Minute)
	current := throughputForProcess(got, "session-current")
	if len(got) != 2 || !current.RateKnown ||
		current.FetchedPagesPerMinute != 20 || current.IndexedPagesPerMinute != 12 {
		t.Fatalf(
			"throughput after current session samples = %+v, want 20 fetched and 12 indexed/minute",
			got,
		)
	}
}

func throughputForProcess(workers []WorkerThroughput, processIdentifier string) WorkerThroughput {
	for _, worker := range workers {
		if worker.ProcessIdentifier == processIdentifier {
			return worker
		}
	}

	return WorkerThroughput{}
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
	now = now.Add(2 * time.Second)
	registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "run-future", WorkerID: "worker-future", State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 1, Indexed: 1},
	})
	if got := registry.Throughput(time.Minute); len(got) != 1 || !got[0].RateKnown {
		t.Fatalf("throughput after clock recovery = %+v, want the retained sample window", got)
	}
}
