package frontier_test

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestAbandonReturnsClaimedJobWithoutSettlingRun(t *testing.T) {
	f := frontier.NewFrontier(1, nil, frontier.WithMaxHostConcurrency(1))
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        0,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	finished := make(chan struct{}, 1)
	seeded := f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, "https://example.com/unfinished"),
		[]byte("abandoned"),
		profile,
		func(bool) { finished <- struct{}{} },
	)
	first := receiveJob(t, f)
	f.Abandon(first)
	if got := f.RunPending(seeded.RunID); got != 1 {
		t.Fatalf("pending = %d, want 1", got)
	}
	select {
	case <-finished:
		t.Fatal("abandoned run settled")
	default:
	}
	second := receiveJob(t, f)
	if second.URL != first.URL {
		t.Fatalf("returned URL = %q, want %q", second.URL, first.URL)
	}
	if second.ObservationID == "" || second.ObservationID != first.ObservationID ||
		second.ObservedAt.IsZero() || !second.ObservedAt.Equal(first.ObservedAt) {
		t.Fatalf("returned observation = (%q, %v), want (%q, %v)",
			second.ObservationID, second.ObservedAt, first.ObservationID, first.ObservedAt)
	}
	f.Done(second, successfulPageOutcome())
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("completed returned job did not settle")
	}
}
