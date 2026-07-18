package runtally_test

import (
	"sync"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/runtally"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestTallyAccumulatesPerRun(t *testing.T) {
	tally := runtally.New()
	tally.Commit([]byte("a"), yagocrawlcontract.CrawlRunTally{Fetched: 1, Indexed: 1})
	tally.Commit([]byte("a"), yagocrawlcontract.CrawlRunTally{
		Fetched: 1, Failed: 1, RobotsDenied: 1, Duplicates: 1,
	})
	tally.Commit([]byte("b"), yagocrawlcontract.CrawlRunTally{Fetched: 1})

	a := tally.Snapshot([]byte("a"))
	if a.Fetched != 2 || a.Indexed != 1 || a.Failed != 1 ||
		a.RobotsDenied != 1 || a.Duplicates != 1 {
		t.Fatalf("run a = %+v", a)
	}
	b := tally.Snapshot([]byte("b"))
	if b.Fetched != 1 || b.Indexed != 0 {
		t.Fatalf("run b = %+v", b)
	}
}

func TestTallyForgetDropsRun(t *testing.T) {
	tally := runtally.New()
	tally.Commit([]byte("a"), yagocrawlcontract.CrawlRunTally{Fetched: 1})
	tally.Forget([]byte("a"))
	if snap := tally.Snapshot([]byte("a")); snap.Fetched != 0 {
		t.Fatalf("forgotten run = %+v, want zero", snap)
	}
}

func TestTallyUnknownRunIsZero(t *testing.T) {
	tally := runtally.New()
	if snap := tally.Snapshot([]byte("missing")); snap != (yagocrawlcontract.CrawlRunTally{}) {
		t.Fatalf("unknown run = %+v, want zero", snap)
	}
}

func TestTallyConcurrentBumpsAreSafe(t *testing.T) {
	tally := runtally.New()
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tally.Commit([]byte("a"), yagocrawlcontract.CrawlRunTally{Fetched: 1})
		}()
	}
	wg.Wait()
	if snap := tally.Snapshot([]byte("a")); snap.Fetched != 50 {
		t.Fatalf("concurrent fetched = %d, want 50", snap.Fetched)
	}
}

func TestTallyRestoreReplacesMemoryWithDurableSnapshot(t *testing.T) {
	tally := runtally.New()
	tally.Commit([]byte("a"), yagocrawlcontract.CrawlRunTally{Fetched: 9, Failed: 7})
	durable := yagocrawlcontract.CrawlRunTally{
		Fetched: 2, Indexed: 1, Duplicates: 3, Pending: 41,
	}
	tally.Restore([]byte("a"), durable)
	durable.Pending = 0
	if got := tally.Snapshot([]byte("a")); got != durable {
		t.Fatalf("restored tally = %+v, want exact durable snapshot %+v", got, durable)
	}
}
