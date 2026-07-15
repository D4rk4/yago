package yagonode

import (
	"context"
	"errors"
	"fmt"
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
			{
				Title: "local", URL: "http://a/1", DisplayURL: "a/1",
				Host: "a", Language: "en", Source: searchcore.SourceGlobal,
			},
			{
				Title: "web", URL: "http://b/2", DisplayURL: "b/2",
				Host: "b", Language: "en", Source: searchcore.SourceWeb,
			},
			{
				Title: "peer", URL: "http://c/3", DisplayURL: "c/3",
				Host: "c", Language: "ru", Source: searchcore.SourceRemote,
			},
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
	if searcher.gotRequest.Verify != searchcore.VerifyIfExist {
		t.Fatalf("verify = %q, want ifexist", searcher.gotRequest.Verify)
	}
	if searcher.gotRequest.WithFacets || len(results.Facets) != 2 {
		t.Fatalf(
			"portal facets request=%v results=%+v",
			searcher.gotRequest.WithFacets,
			results.Facets,
		)
	}
	for _, facet := range results.Facets {
		if facet.Scope != "this visible result window" {
			t.Fatalf("derived facet scope = %q", facet.Scope)
		}
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

func TestPortalSourceCarriesBoundedPartialFailureState(t *testing.T) {
	t.Parallel()

	searcher := &stubPortalSearcher{response: searchcore.Response{
		PartialFailures: []searchcore.PartialFailure{
			{Source: searchcore.PartialFailureSourceRemoteStage, Reason: "capacity detail"},
			{Source: searchcore.PartialFailureSourceWeb, Reason: "provider detail"},
			{Source: "AAAAAAAAAAAA", Reason: "peer detail"},
			{Source: "AAAAAAAAAAAA", Reason: "duplicate peer detail"},
		},
	}}
	results, err := newPortalSource(searcher).Search(t.Context(), "go", "", 0, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !results.Incomplete || !results.FederationUnavailable || results.PeersFailed != 1 {
		t.Fatalf("partial state = %+v", results)
	}
}

func TestPortalSourceLabelsCorpusFacetPopulation(t *testing.T) {
	t.Parallel()

	searcher := &stubPortalSearcher{response: searchcore.Response{
		Facets: []searchcore.FacetGroup{{
			Name: "host", Terms: []searchcore.FacetTerm{{Term: "example.org", Count: 42}},
		}},
	}}
	results, err := newPortalSource(searcher).Search(t.Context(), "go", "", 0, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results.Facets) != 1 || results.Facets[0].Scope != "the local corpus" {
		t.Fatalf("corpus facet state = %+v", results.Facets)
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

func TestPortalSourceCarriesTotalMissSuggestion(t *testing.T) {
	t.Parallel()

	searcher := &stubPortalSearcher{response: searchcore.Response{
		DidYouMean: "golang",
	}}
	results, err := newPortalSource(searcher).Search(
		context.Background(),
		"golnag",
		"",
		0,
		10,
	)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if results.Recovered || results.DidYouMean != "golang" ||
		results.DidYouMeanURL != "/?q=golang" {
		t.Fatalf("miss suggestion = %+v", results)
	}
}

func TestPortalSourceHintsFilterOnlyZeroResult(t *testing.T) {
	t.Parallel()

	searcher := &stubPortalSearcher{response: searchcore.Response{
		TotalResults: 0,
		Request:      searchcore.Request{FileType: "pdf"},
	}}
	hinted, err := newPortalSource(searcher).Search(context.Background(), "filetype:pdf", "", 0, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if hinted.Hint == "" {
		t.Fatal("a filter-only zero-result query must carry a browse hint")
	}

	withResults := &stubPortalSearcher{response: searchcore.Response{
		TotalResults: 1,
		Results:      []searchcore.Result{{Title: "x", URL: "http://a/1"}},
		Request:      searchcore.Request{FileType: "pdf", Terms: []string{"go"}},
	}}
	plain, err := newPortalSource(withResults).Search(
		context.Background(), "go filetype:pdf", "", 0, 10,
	)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if plain.Hint != "" {
		t.Fatalf("a query with results must carry no hint, got %q", plain.Hint)
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

// TestFacetsFromResultsBackfillPeerAnswers pins SEARCH-36: a page answered by
// peers or the web carries no corpus facet counts, so the sidebar derives its
// groups from the visible rows instead of disappearing.
func TestFacetsFromResultsBackfillPeerAnswers(t *testing.T) {
	results := []searchcore.Result{
		{URL: "https://a.example/1", Host: "a.example", Language: "ru"},
		{URL: "https://a.example/2", Host: "a.example", Language: "ru"},
		{URL: "https://b.example/1", Host: "b.example", Language: "EN"},
		{URL: "https://c.example/1"},
	}
	groups := facetsFromResults(results)
	if len(groups) != 2 || groups[0].Name != "host" || groups[1].Name != "language" {
		t.Fatalf("groups = %+v", groups)
	}
	if groups[0].Terms[0].Term != "a.example" || groups[0].Terms[0].Count != 2 {
		t.Fatalf("host tally = %+v", groups[0].Terms)
	}
	if groups[1].Terms[0].Term != "ru" || groups[1].Terms[0].Count != 2 ||
		groups[1].Terms[1].Term != "en" {
		t.Fatalf("language tally = %+v", groups[1].Terms)
	}

	if got := facetsFromResults(nil); len(got) != 0 {
		t.Fatalf("empty rows must yield no groups: %+v", got)
	}
}

func TestFacetGroupFromCountsCapsAndOrders(t *testing.T) {
	counts := map[string]int{}
	for i := range 12 {
		counts[fmt.Sprintf("host%02d.example", i)] = 1
	}
	counts["popular.example"] = 5
	group, ok := facetGroupFromCounts("host", counts)
	if !ok || len(group.Terms) != facetsFromResultsCap {
		t.Fatalf("group = %+v ok=%v", group, ok)
	}
	if group.Terms[0].Term != "popular.example" {
		t.Fatalf("ordering wrong: %+v", group.Terms[0])
	}
}
