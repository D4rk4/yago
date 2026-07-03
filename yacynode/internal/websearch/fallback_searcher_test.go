package websearch

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yacynode/internal/searchcore"
)

type stubSearcher struct {
	resp  searchcore.Response
	err   error
	calls int
}

func (s *stubSearcher) Search(context.Context, searchcore.Request) (searchcore.Response, error) {
	s.calls++

	return s.resp, s.err
}

type stubProvider struct {
	results  []Result
	err      error
	calls    int
	gotQuery string
	gotLimit int
}

func (p *stubProvider) Search(_ context.Context, query string, limit int) ([]Result, error) {
	p.calls++
	p.gotQuery = query
	p.gotLimit = limit

	return p.results, p.err
}

func enabled() bool  { return true }
func disabled() bool { return false }

func TestFallbackSkippedWhenPrimaryHasResults(t *testing.T) {
	primary := &stubSearcher{
		resp: searchcore.Response{Results: []searchcore.Result{{Title: "owned"}}},
	}
	provider := &stubProvider{results: []Result{{Title: "web"}}}
	searcher := NewFallbackSearcher(primary, provider, enabled)

	resp, err := searcher.Search(context.Background(), searchcore.Request{Query: "x", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if provider.calls != 0 {
		t.Error("provider must not run when the primary has results")
	}
	if len(resp.Results) != 1 || resp.Results[0].Title != "owned" {
		t.Errorf("results = %#v", resp.Results)
	}
}

func TestFallbackRunsOnMiss(t *testing.T) {
	primary := &stubSearcher{}
	provider := &stubProvider{results: []Result{
		{Title: "web one", URL: "https://a.example.com/x", Snippet: "s1"},
		{Title: "web two", URL: "https://b.example.com/y", Snippet: "s2"},
	}}
	searcher := NewFallbackSearcher(primary, provider, enabled)

	resp, err := searcher.Search(context.Background(), searchcore.Request{Query: "gap", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if provider.calls != 1 || provider.gotQuery != "gap" || provider.gotLimit != 10 {
		t.Fatalf("provider call = %#v", provider)
	}
	if len(resp.Results) != 2 || resp.TotalResults != 2 {
		t.Fatalf("results = %#v total=%d", resp.Results, resp.TotalResults)
	}
	first := resp.Results[0]
	if first.Source != searchcore.SourceWeb || first.Host != "a.example.com" {
		t.Errorf("first result = %#v", first)
	}
	if resp.Results[0].Score <= resp.Results[1].Score {
		t.Errorf("scores should decay: %v %v", resp.Results[0].Score, resp.Results[1].Score)
	}
}

func TestFallbackSkippedWhenDisabled(t *testing.T) {
	primary := &stubSearcher{}
	provider := &stubProvider{results: []Result{{Title: "web"}}}
	searcher := NewFallbackSearcher(primary, provider, disabled)

	resp, err := searcher.Search(context.Background(), searchcore.Request{Query: "gap", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if provider.calls != 0 {
		t.Error("provider must not run when disabled")
	}
	if len(resp.Results) != 0 {
		t.Errorf("results = %#v", resp.Results)
	}
}

func TestFallbackSkippedWhenQueryBlank(t *testing.T) {
	primary := &stubSearcher{}
	provider := &stubProvider{results: []Result{{Title: "web"}}}
	searcher := NewFallbackSearcher(primary, provider, enabled)

	if _, err := searcher.Search(
		context.Background(),
		searchcore.Request{Query: "  ", Limit: 10},
	); err != nil {
		t.Fatalf("search: %v", err)
	}
	if provider.calls != 0 {
		t.Error("provider must not run for a blank query")
	}
}

func TestFallbackDegradesOnProviderError(t *testing.T) {
	primary := &stubSearcher{}
	provider := &stubProvider{err: errors.New("boom")}
	searcher := NewFallbackSearcher(primary, provider, enabled)

	resp, err := searcher.Search(context.Background(), searchcore.Request{Query: "gap", Limit: 10})
	if err != nil {
		t.Fatalf("provider error must be swallowed, got %v", err)
	}
	if len(resp.Results) != 0 {
		t.Errorf("results = %#v, want empty", resp.Results)
	}
}

func TestFallbackPropagatesPrimaryError(t *testing.T) {
	sentinel := errors.New("primary down")
	primary := &stubSearcher{err: sentinel}
	provider := &stubProvider{}
	searcher := NewFallbackSearcher(primary, provider, enabled)

	if _, err := searcher.Search(
		context.Background(),
		searchcore.Request{Query: "x", Limit: 10},
	); !errors.Is(
		err,
		sentinel,
	) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
	if provider.calls != 0 {
		t.Error("provider must not run when the primary errored")
	}
}

func TestToCoreResultsCapsToLimit(t *testing.T) {
	results := toCoreResults([]Result{
		{URL: "https://a/"}, {URL: "https://b/"}, {URL: "https://c/"},
	}, 2)
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
}
