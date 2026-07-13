package yagonode

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/urldenylist"
)

type fakeSearcher struct {
	resp     searchcore.Response
	err      error
	calls    int
	complete func()
}

func (f *fakeSearcher) Search(context.Context, searchcore.Request) (searchcore.Response, error) {
	f.calls++
	if f.complete != nil {
		f.complete()
	}

	return f.resp, f.err
}

type fakeSnapshotter struct {
	snap urldenylist.Snapshot
}

func (f fakeSnapshotter) Snapshot() urldenylist.Snapshot {
	return f.snap
}

func openDenylistStore(t *testing.T, entries map[urldenylist.Kind][]string) *urldenylist.Store {
	t.Helper()
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	store, err := urldenylist.Open(v, func() time.Time { return time.Unix(1, 0) })
	if err != nil {
		t.Fatalf("urldenylist.Open: %v", err)
	}
	for kind, values := range entries {
		for _, value := range values {
			if err := store.Add(context.Background(), kind, value); err != nil {
				t.Fatalf("seed denylist: %v", err)
			}
		}
	}

	return store
}

func resultsResponse(total int, urls ...string) searchcore.Response {
	results := make([]searchcore.Result, 0, len(urls))
	for _, url := range urls {
		results = append(results, searchcore.Result{URL: url})
	}

	return searchcore.Response{TotalResults: total, Results: results}
}

func TestDenylistFilterRemovesBlockedResults(t *testing.T) {
	store := openDenylistStore(t, map[urldenylist.Kind][]string{
		urldenylist.KindDomain: {"blocked.example"},
		urldenylist.KindURL:    {"https://ok.example/bad"},
	})
	next := &fakeSearcher{resp: resultsResponse(3,
		"https://blocked.example/x",
		"https://ok.example/bad",
		"https://ok.example/good",
	)}

	resp, err := withDenylistFilter(next, store).Search(context.Background(), searchcore.Request{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].URL != "https://ok.example/good" {
		t.Fatalf("results = %#v", resp.Results)
	}
	if resp.TotalResults != 1 {
		t.Fatalf("total = %d, want 1", resp.TotalResults)
	}
}

func TestDenylistFilterUsesSnapshotAfterSearchContextEnds(t *testing.T) {
	store := openDenylistStore(t, map[urldenylist.Kind][]string{
		urldenylist.KindDomain: {"blocked.example"},
	})
	next := &fakeSearcher{resp: resultsResponse(2,
		"https://blocked.example/x",
		"https://ok.example/good",
	)}
	ctx, cancel := context.WithCancel(t.Context())
	next.complete = cancel
	resp, err := withDenylistFilter(next, store).Search(ctx, searchcore.Request{})
	if err != nil || len(resp.Results) != 1 || resp.Results[0].URL != "https://ok.example/good" {
		t.Fatalf("canceled-context results = %#v, error = %v", resp.Results, err)
	}
}

func TestDenylistFilterNilPassesThrough(t *testing.T) {
	next := &fakeSearcher{}
	if withDenylistFilter(next, nil) != searchcore.Searcher(next) {
		t.Fatal("a nil denylist should return the searcher unchanged")
	}
}

func TestDenylistFilterPropagatesSearchError(t *testing.T) {
	next := &fakeSearcher{err: errors.New("boom")}
	store := openDenylistStore(t, nil)

	if _, err := withDenylistFilter(
		next,
		store,
	).Search(context.Background(), searchcore.Request{}); err == nil {
		t.Fatal("a search error should propagate")
	}
}

func TestDenylistFilterEmptySnapshotPassesResults(t *testing.T) {
	next := &fakeSearcher{resp: resultsResponse(2, "https://a.example/", "https://b.example/")}
	deny := fakeSnapshotter{} // zero-value snapshot is empty

	resp, err := withDenylistFilter(next, deny).Search(context.Background(), searchcore.Request{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(resp.Results) != 2 || resp.TotalResults != 2 {
		t.Fatalf(
			"an empty denylist should not change results: %#v (%d)",
			resp.Results,
			resp.TotalResults,
		)
	}
}

func TestDenylistFilterFloorsTotalResults(t *testing.T) {
	store := openDenylistStore(t, map[urldenylist.Kind][]string{
		urldenylist.KindDomain: {"blocked.example"},
	})
	// TotalResults is deliberately smaller than the number removed.
	next := &fakeSearcher{resp: resultsResponse(1,
		"https://blocked.example/1",
		"https://blocked.example/2",
	)}

	resp, err := withDenylistFilter(next, store).Search(context.Background(), searchcore.Request{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(resp.Results) != 0 || resp.TotalResults != 0 {
		t.Fatalf("total should floor at 0: results=%#v total=%d", resp.Results, resp.TotalResults)
	}
}
