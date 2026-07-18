package crawlruns

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestRegistryRetainsEveryActiveRunBeyondRecentCapacity(t *testing.T) {
	const activeRuns = 300
	registry := New(256)
	base := time.Unix(7000, 0)
	now := base
	registry.now = func() time.Time { return now }
	lastObservedActive := 0
	registry.AddObserver(func(_ Run, _ bool, active int) {
		lastObservedActive = active
	})
	for index := range activeRuns {
		now = base.Add(time.Duration(index) * time.Second)
		registry.Record(t.Context(), yagocrawlcontract.CrawlRunProgress{
			RunID: fmt.Sprintf("active-%03d", index),
			State: yagocrawlcontract.CrawlRunRunning,
		})
	}
	if registry.Len() != activeRuns || lastObservedActive != activeRuns {
		t.Fatalf(
			"retained/observed active runs = %d/%d, want %d/%d",
			registry.Len(),
			lastObservedActive,
			activeRuns,
			activeRuns,
		)
	}
	first := runByID(t, registry.Recent(), "active-000")
	now = base.Add(time.Hour)
	registry.Record(t.Context(), yagocrawlcontract.CrawlRunProgress{
		RunID: "active-000", State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 1},
	})
	updated := runByID(t, registry.Recent(), "active-000")
	if registry.Len() != activeRuns || !updated.FirstSeen.Equal(first.FirstSeen) ||
		updated.Tally.Fetched != 1 {
		t.Fatalf(
			"updated active run len/firstSeen/fetched = %d/%v/%d, want %d/%v/1",
			registry.Len(),
			updated.FirstSeen,
			updated.Tally.Fetched,
			activeRuns,
			first.FirstSeen,
		)
	}
}

func TestRegistryCapacityBoundsOnlyTerminalHistory(t *testing.T) {
	registry := New(2)
	base := time.Unix(8000, 0)
	now := base
	registry.now = func() time.Time { return now }
	recordAt := func(
		id string,
		state yagocrawlcontract.CrawlRunState,
		offset time.Duration,
	) {
		now = base.Add(offset)
		registry.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
			RunID: id, State: state,
		})
	}
	recordAt("active-old", yagocrawlcontract.CrawlRunRunning, 0)
	recordAt("terminal-a", yagocrawlcontract.CrawlRunFinished, time.Second)
	recordAt("terminal-b", yagocrawlcontract.CrawlRunCancelled, 2*time.Second)
	recordAt("terminal-c", yagocrawlcontract.CrawlRunFinished, 3*time.Second)
	assertRunIDs(t, registry.Recent(), "terminal-c", "terminal-b", "active-old")
	recordAt("active-old", yagocrawlcontract.CrawlRunFinished, 4*time.Second)
	assertRunIDs(t, registry.Recent(), "active-old", "terminal-c")
}

func TestRegistryTerminalHistoryEvictionBreaksTimestampTiesByRunID(t *testing.T) {
	registry := New(1)
	fixed := time.Unix(9000, 0)
	registry.now = func() time.Time { return fixed }
	for _, runID := range []string{"terminal-b", "terminal-a"} {
		registry.Record(t.Context(), yagocrawlcontract.CrawlRunProgress{
			RunID: runID, State: yagocrawlcontract.CrawlRunFinished,
		})
	}
	assertRunIDs(t, registry.Recent(), "terminal-b")
}

func runByID(t *testing.T, runs []Run, runID string) Run {
	t.Helper()
	for _, run := range runs {
		if run.RunID == runID {
			return run
		}
	}
	t.Fatalf("run %q is missing from %d records", runID, len(runs))

	return Run{}
}

func assertRunIDs(t *testing.T, runs []Run, want ...string) {
	t.Helper()
	got := make([]string, len(runs))
	for index, run := range runs {
		got[index] = run.RunID
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("run ids = %v, want %v", got, want)
	}
}
