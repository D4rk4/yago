package runtally_test

import (
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/runtally"
)

func TestTallyAccumulatesPerRun(t *testing.T) {
	tally := runtally.New()
	tally.Fetched([]byte("a"))
	tally.Fetched([]byte("a"))
	tally.Indexed([]byte("a"))
	tally.Failed([]byte("a"))
	tally.RobotsDenied([]byte("a"))
	tally.Duplicate([]byte("a"))
	tally.Fetched([]byte("b"))

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
	tally.Fetched([]byte("a"))
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
			tally.Fetched([]byte("a"))
		}()
	}
	wg.Wait()
	if snap := tally.Snapshot([]byte("a")); snap.Fetched != 50 {
		t.Fatalf("concurrent fetched = %d, want 50", snap.Fetched)
	}
}
