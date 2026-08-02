package websearch

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

// TestFallbackSkipsFirstSeenBoundedRequests pins the decision that a request
// bounding this node's own discovery time does not buy an external search. The
// provider's rows are not held here, so the caller's window drops every one of
// them; running it would expose the query to an external engine and spend the
// shared provider budget for nothing. Either half of the window is enough, and
// the seeding side effect goes with the call, which is why the unbounded control
// below is part of the same guard.
func TestFallbackSkipsFirstSeenBoundedRequests(t *testing.T) {
	when := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)
	for name, req := range map[string]searchcore.Request{
		"start":  {Query: "gap", Limit: 10, MinFirstSeen: when},
		"end":    {Query: "gap", Limit: 10, MaxFirstSeen: when},
		"closed": {Query: "gap", Limit: 10, MinFirstSeen: when, MaxFirstSeen: when.Add(time.Hour)},
	} {
		t.Run(name, func(t *testing.T) {
			provider := &stubProvider{
				results: []Result{{URL: "https://web.example/gap", Title: "Web gap"}},
			}
			seeder := &stubSeeder{}
			searcher := NewFallbackSearcher(&stubSearcher{}, provider, enabled, WithSeeder(seeder))

			resp, err := searcher.Search(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if provider.calls != 0 || len(resp.Results) != 0 || seeder.calls != 0 {
				t.Fatalf(
					"provider calls = %d, results = %#v, seeder calls = %d",
					provider.calls, resp.Results, seeder.calls,
				)
			}
		})
	}
}

// TestFallbackStillRunsWithoutAFirstSeenBound is the over-refusing half: the
// suppression above must key off the first-seen window and nothing else, or an
// ordinary empty search stops recovering from the web.
func TestFallbackStillRunsWithoutAFirstSeenBound(t *testing.T) {
	provider := &stubProvider{
		results: []Result{{URL: "https://web.example/gap", Title: "Web gap"}},
	}
	searcher := NewFallbackSearcher(&stubSearcher{}, provider, enabled)

	resp, err := searcher.Search(
		context.Background(),
		searchcore.Request{Query: "gap", Limit: 10},
	)
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 || len(resp.Results) != 1 {
		t.Fatalf("provider calls = %d, results = %#v", provider.calls, resp.Results)
	}
}

// TestFallbackStillRunsUnderAPublicationWindow separates the two dimensions at
// this gate too. A publication window can be answered by a provider row, whose
// own date is then checked, so it keeps buying the call; only the first-seen
// window, which nothing outside this node can satisfy, suppresses it.
func TestFallbackStillRunsUnderAPublicationWindow(t *testing.T) {
	provider := &stubProvider{
		results: []Result{{URL: "https://web.example/gap", Title: "Web gap"}},
	}
	searcher := NewFallbackSearcher(&stubSearcher{}, provider, enabled)

	if _, err := searcher.Search(
		context.Background(),
		searchcore.Request{
			Query:   "gap",
			Limit:   10,
			MinDate: time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC),
			MaxDate: time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC),
		},
	); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want a publication window to keep the call", provider.calls)
	}
}
