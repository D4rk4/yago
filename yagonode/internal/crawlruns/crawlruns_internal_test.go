package crawlruns

import (
	"context"
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

func TestRegistryEvictsOldestOverCapacity(t *testing.T) {
	reg := New(2)
	base := time.Unix(3000, 0)
	tick := base
	reg.now = func() time.Time { return tick }
	ctx := context.Background()

	for i, id := range []string{"a", "b", "c"} {
		tick = base.Add(time.Duration(i) * time.Second)
		reg.Record(ctx, yagocrawlcontract.CrawlRunProgress{RunID: id})
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
