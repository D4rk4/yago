package crawlruns

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestRegistryRecordUpsertsAndPreservesFirstSeen(t *testing.T) {
	reg := New(4)
	base := time.Unix(1000, 0)
	tick := base
	reg.now = func() time.Time { return tick }
	ctx := context.Background()

	reg.Record(ctx, yagocrawlcontract.CrawlRunProgress{
		RunID: "a", ProfileName: "docs", State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 2},
	})
	tick = base.Add(time.Minute)
	reg.Record(ctx, yagocrawlcontract.CrawlRunProgress{
		RunID: "a", ProfileName: "docs", State: yagocrawlcontract.CrawlRunFinished,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 5, Indexed: 4},
	})

	runs := reg.Recent()
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	run := runs[0]
	if !run.FirstSeen.Equal(base) {
		t.Fatalf("first seen = %v, want %v", run.FirstSeen, base)
	}
	if !run.Updated.Equal(base.Add(time.Minute)) {
		t.Fatalf("updated = %v", run.Updated)
	}
	if run.State != yagocrawlcontract.CrawlRunFinished ||
		run.Tally.Fetched != 5 || run.Tally.Indexed != 4 {
		t.Fatalf("run = %+v, want finished snapshot", run)
	}
}

func TestRegistrySetRateRemembersRateAcrossReports(t *testing.T) {
	reg := New(4)
	ctx := context.Background()
	reg.Record(ctx, yagocrawlcontract.CrawlRunProgress{
		RunID: "a", State: yagocrawlcontract.CrawlRunRunning,
	})

	reg.SetRate("a", 45)
	if got := reg.Recent()[0]; got.PagesPerMinute != 45 || !got.RateKnown {
		t.Fatalf("rate after SetRate = %d/%v, want known 45", got.PagesPerMinute, got.RateKnown)
	}

	reg.Record(ctx, yagocrawlcontract.CrawlRunProgress{
		RunID: "a", State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{Fetched: 9},
	})
	run := reg.Recent()[0]
	if run.PagesPerMinute != 45 || run.Tally.Fetched != 9 {
		t.Fatalf("run = %+v, want rate 45 preserved with fresh tally", run)
	}

	reg.SetRate("a", 0)
	if got := reg.Recent()[0]; got.PagesPerMinute != 0 || !got.RateKnown {
		t.Fatalf(
			"rate after lifting throttle = %d/%v, want known 0",
			got.PagesPerMinute,
			got.RateKnown,
		)
	}
}

func TestRegistryReconcilesWorkerEffectiveRate(t *testing.T) {
	t.Parallel()

	reg := New(4)
	reg.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "default", PagesPerMinute: 30, RateKnown: true,
	})
	if got := reg.Recent()[0]; got.PagesPerMinute != 30 || !got.RateKnown {
		t.Fatalf("default rate = %d/%v, want known 30", got.PagesPerMinute, got.RateKnown)
	}

	reg.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "default", PagesPerMinute: 0, RateKnown: true,
	})
	if got := reg.Recent()[0]; got.PagesPerMinute != 0 || !got.RateKnown {
		t.Fatalf("unlimited rate = %d/%v, want known 0", got.PagesPerMinute, got.RateKnown)
	}
}

func TestRegistrySetRateIgnoresUnknownAndEmptyRun(t *testing.T) {
	reg := New(4)
	reg.SetRate("missing", 30)
	reg.SetRate("", 30)
	if got := reg.Len(); got != 0 {
		t.Fatalf("registry length = %d, want 0 (SetRate must not create runs)", got)
	}
}

func TestRegistryRecentOrdersByUpdatedDesc(t *testing.T) {
	reg := New(4)
	base := time.Unix(2000, 0)
	tick := base
	reg.now = func() time.Time { return tick }
	ctx := context.Background()

	for i, id := range []string{"a", "b", "c"} {
		tick = base.Add(time.Duration(i) * time.Second)
		reg.Record(ctx, yagocrawlcontract.CrawlRunProgress{RunID: id})
	}

	runs := reg.Recent()
	got := []string{runs[0].RunID, runs[1].RunID, runs[2].RunID}
	want := []string{"c", "b", "a"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestRegistryRecentBreaksUpdatedTiesByRunID(t *testing.T) {
	reg := New(4)
	fixed := time.Unix(5000, 0)
	reg.now = func() time.Time { return fixed }
	ctx := context.Background()

	reg.Record(ctx, yagocrawlcontract.CrawlRunProgress{RunID: "b"})
	reg.Record(ctx, yagocrawlcontract.CrawlRunProgress{RunID: "a"})

	runs := reg.Recent()
	if len(runs) != 2 || runs[0].RunID != "a" || runs[1].RunID != "b" {
		t.Fatalf("order = %v, want [a b] with equal Updated broken by run id",
			[]string{runs[0].RunID, runs[1].RunID})
	}
}

func TestRegistryEvictsOldestTerminalOverCapacity(t *testing.T) {
	reg := New(2)
	base := time.Unix(3000, 0)
	tick := base
	reg.now = func() time.Time { return tick }
	ctx := context.Background()

	for i, id := range []string{"a", "b", "c"} {
		tick = base.Add(time.Duration(i) * time.Second)
		reg.Record(ctx, yagocrawlcontract.CrawlRunProgress{
			RunID: id, State: yagocrawlcontract.CrawlRunFinished,
		})
	}

	if reg.Len() != 2 {
		t.Fatalf("len = %d, want 2 (capacity)", reg.Len())
	}
	runs := reg.Recent()
	if runs[0].RunID != "c" || runs[1].RunID != "b" {
		t.Fatalf("remaining = %v, want [c b]", []string{runs[0].RunID, runs[1].RunID})
	}
}

func TestRegistryIgnoresEmptyRunID(t *testing.T) {
	reg := New(0)
	reg.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{RunID: ""})
	if reg.Len() != 0 {
		t.Fatalf("len = %d, want 0", reg.Len())
	}
}

func TestRegistryRecordsRepeatedAttemptsWithTheSameRunID(t *testing.T) {
	reg := New(4)
	var transitions []struct {
		state         yagocrawlcontract.CrawlRunState
		newlyTerminal bool
		active        int
	}
	reg.AddObserver(func(run Run, newlyTerminal bool, active int) {
		transitions = append(transitions, struct {
			state         yagocrawlcontract.CrawlRunState
			newlyTerminal bool
			active        int
		}{run.State, newlyTerminal, active})
	})
	reg.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "settled",
		State: yagocrawlcontract.CrawlRunFinished,
		Tally: yagocrawlcontract.CrawlRunTally{Indexed: 4},
	})
	reg.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "settled",
		State: yagocrawlcontract.CrawlRunRunning,
		Tally: yagocrawlcontract.CrawlRunTally{Indexed: 1, Pending: 3},
	})
	reg.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "settled",
		State: yagocrawlcontract.CrawlRunCancelled,
		Tally: yagocrawlcontract.CrawlRunTally{Indexed: 2},
	})

	runs := reg.Recent()
	if len(runs) != 1 || runs[0].State != yagocrawlcontract.CrawlRunCancelled ||
		runs[0].Tally.Indexed != 2 || runs[0].Tally.Pending != 0 {
		t.Fatalf("repeated run = %+v", runs)
	}
	want := []struct {
		state         yagocrawlcontract.CrawlRunState
		newlyTerminal bool
		active        int
	}{
		{yagocrawlcontract.CrawlRunFinished, true, 0},
		{yagocrawlcontract.CrawlRunRunning, false, 1},
		{yagocrawlcontract.CrawlRunCancelled, true, 0},
	}
	if !reflect.DeepEqual(transitions, want) {
		t.Fatalf("transitions = %+v, want %+v", transitions, want)
	}
}
