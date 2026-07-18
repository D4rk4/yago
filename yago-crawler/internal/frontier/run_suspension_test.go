package frontier_test

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestSuspendDrainsMemoryWithoutMarkingRunCancelled(t *testing.T) {
	f := frontier.NewFrontier(2, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        0,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	provenance := []byte("suspended-run")
	finished := make(chan bool, 1)
	f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, "https://example.com/a"),
		provenance,
		profile,
		func(succeeded bool) { finished <- succeeded },
	)

	f.Suspend(provenance)
	f.WaitForSettlements()

	if !f.WasSuspended(provenance) {
		t.Fatal("run was not marked suspended")
	}
	if f.WasCancelled(provenance) {
		t.Fatal("suspended run was marked cancelled")
	}
	select {
	case succeeded := <-finished:
		if !succeeded {
			t.Fatal("suspension changed the run delivery outcome")
		}
	case <-time.After(time.Second):
		t.Fatal("suspended run did not settle")
	}
	f.ClearSuspended(provenance)
	if f.WasSuspended(provenance) {
		t.Fatal("suspended marker was not cleared")
	}
}
