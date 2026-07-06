package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type stubPortalSearcher struct {
	response   searchcore.Response
	err        error
	gotRequest searchcore.Request
}

func (s *stubPortalSearcher) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	s.gotRequest = req

	return s.response, s.err
}

func TestPortalSourceMapsAndMarksResults(t *testing.T) {
	t.Parallel()

	searcher := &stubPortalSearcher{response: searchcore.Response{
		TotalResults: 3,
		Results: []searchcore.Result{
			// A local hit in a global search carries the request source.
			{Title: "local", URL: "http://a/1", DisplayURL: "a/1", Source: searchcore.SourceGlobal},
			{Title: "web", URL: "http://b/2", DisplayURL: "b/2", Source: searchcore.SourceWeb},
			{Title: "peer", URL: "http://c/3", DisplayURL: "c/3", Source: searchcore.SourceRemote},
		},
	}}

	results, err := newPortalSource(searcher).Search(context.Background(), "go", "", 20, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if results.TotalResults != 3 || len(results.Results) != 3 {
		t.Fatalf("unexpected results: %+v", results)
	}
	if searcher.gotRequest.Offset != 20 || searcher.gotRequest.Limit != 10 {
		t.Fatalf("offset=%d limit=%d, want the window forwarded to the searcher",
			searcher.gotRequest.Offset, searcher.gotRequest.Limit)
	}
	if searcher.gotRequest.Source != searchcore.SourceGlobal {
		t.Fatalf("source = %q, want global", searcher.gotRequest.Source)
	}
	if results.LocalCount != 1 || results.WebCount != 1 || results.PeerCount != 1 {
		t.Fatalf("provenance counts = %d/%d/%d, want 1/1/1",
			results.LocalCount, results.PeerCount, results.WebCount)
	}
	if results.Results[0].Provenance != "local" ||
		results.Results[1].Provenance != "web" ||
		results.Results[2].Provenance != "peer" {
		t.Fatalf("provenance labels = %q/%q/%q",
			results.Results[0].Provenance,
			results.Results[1].Provenance,
			results.Results[2].Provenance)
	}
	if results.Results[0].CachedURL == "" {
		t.Fatal("locally stored result must carry a cached link (global-source local hit)")
	}
	if results.Results[1].CachedURL != "" || results.Results[2].CachedURL != "" {
		t.Fatal("web and peer results must not carry cached links")
	}
}

func TestPortalSourceCarriesRecoverySuggestion(t *testing.T) {
	t.Parallel()

	searcher := &stubPortalSearcher{response: searchcore.Response{
		TotalResults: 1,
		Results:      []searchcore.Result{{Title: "Golang", URL: "http://a/1"}},
		Recovered:    "fuzzy",
		DidYouMean:   "golang tutorial",
	}}

	results, err := newPortalSource(searcher).Search(context.Background(), "golnag", "", 0, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !results.Recovered || results.DidYouMean != "golang tutorial" {
		t.Fatalf("recovery not carried: %+v", results)
	}
	if results.DidYouMeanURL != "/?q=golang+tutorial" {
		t.Fatalf("did-you-mean url = %q", results.DidYouMeanURL)
	}
}

func TestPortalSourceWrapsError(t *testing.T) {
	t.Parallel()

	searcher := &stubPortalSearcher{err: errors.New("boom")}
	if _, err := newPortalSource(
		searcher,
	).Search(context.Background(), "go", "", 0, 10); err == nil {
		t.Fatal("expected error")
	}
}
