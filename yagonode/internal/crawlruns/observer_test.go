package crawlruns

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type observation struct {
	run           Run
	newlyTerminal bool
	active        int
}

func collectObservations(reg *Registry) *[]observation {
	got := &[]observation{}
	reg.AddObserver(func(run Run, newlyTerminal bool, active int) {
		*got = append(*got, observation{run, newlyTerminal, active})
	})

	return got
}

func record(reg *Registry, id string, state yagocrawlcontract.CrawlRunState) {
	reg.Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: id,
		State: state,
	})
}

func TestRegistryObserverDetectsTerminalTransitionOnce(t *testing.T) {
	reg := New(8)
	got := collectObservations(reg)

	record(reg, "r1", yagocrawlcontract.CrawlRunRunning)
	record(reg, "r1", yagocrawlcontract.CrawlRunFinished)
	record(reg, "r1", yagocrawlcontract.CrawlRunFinished)

	if len(*got) != 3 {
		t.Fatalf("observations = %d, want 3", len(*got))
	}
	if (*got)[0].newlyTerminal || (*got)[0].active != 1 {
		t.Fatalf("running obs = %+v, want newlyTerminal=false active=1", (*got)[0])
	}
	if !(*got)[1].newlyTerminal || (*got)[1].active != 0 {
		t.Fatalf("finish obs = %+v, want newlyTerminal=true active=0", (*got)[1])
	}
	if (*got)[2].newlyTerminal {
		t.Fatal("re-finish reported newlyTerminal=true, want false once already terminal")
	}
}

func TestRegistryObserverFirstReportTerminal(t *testing.T) {
	reg := New(8)
	got := collectObservations(reg)

	record(reg, "r1", yagocrawlcontract.CrawlRunCancelled)

	if len(*got) != 1 {
		t.Fatalf("observations = %d, want 1", len(*got))
	}
	if !(*got)[0].newlyTerminal || (*got)[0].active != 0 {
		t.Fatalf("first-terminal obs = %+v, want newlyTerminal=true active=0", (*got)[0])
	}
}

func TestRegistryObserverIgnoresMissingRunID(t *testing.T) {
	reg := New(8)
	got := collectObservations(reg)

	record(reg, "", yagocrawlcontract.CrawlRunRunning)

	if len(*got) != 0 {
		t.Fatalf("observations = %d, want none for empty run id", len(*got))
	}
}

func TestRegistryActiveCountTracksConcurrentRuns(t *testing.T) {
	reg := New(8)
	got := collectObservations(reg)

	record(reg, "r1", yagocrawlcontract.CrawlRunRunning)
	record(reg, "r2", yagocrawlcontract.CrawlRunRunning)
	record(reg, "r1", yagocrawlcontract.CrawlRunFinished)

	last := (*got)[len(*got)-1]
	if last.active != 1 {
		t.Fatalf("active after one of two finished = %d, want 1", last.active)
	}
}
