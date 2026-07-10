package searchlocal

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

type fakeIndex struct {
	response searchindex.SearchResultSet
	err      error
	got      searchindex.SearchRequest
}

func (i *fakeIndex) Index(context.Context, documentstore.Document) error { return nil }

func (i *fakeIndex) Delete(context.Context, string) error { return nil }

func (i *fakeIndex) Search(
	_ context.Context,
	req searchindex.SearchRequest,
) (searchindex.SearchResultSet, error) {
	i.got = req
	if i.err != nil {
		return searchindex.SearchResultSet{}, i.err
	}

	return i.response, nil
}

func (i *fakeIndex) Stats(context.Context) (searchindex.IndexStats, error) {
	return searchindex.IndexStats{}, nil
}

func TestSearcherTranslatesAndFiltersResults(t *testing.T) {
	published := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	index := &fakeIndex{response: searchindex.SearchResultSet{
		Total: 3,
		Results: []searchindex.SearchResult{
			{
				Title:         "Second",
				URL:           "https://docs.example.org/guide.pdf",
				Snippet:       "second snippet",
				Score:         2,
				PublishedDate: published,
			},
			{
				Title:     "First",
				URL:       "https://example.org/file.pdf",
				Snippet:   "first snippet",
				Score:     1,
				Author:    "Ada Lovelace",
				Keywords:  "go, search",
				Publisher: "Example Press",
			},
			{
				Title:   "Rejected",
				URL:     "https://example.net/file.txt",
				Snippet: "rejected",
				Score:   3,
			},
		},
	}}

	resp, err := NewSearcher(index).Search(t.Context(), searchcore.Request{
		Query:            "golang",
		Terms:            []string{"fallback"},
		ExcludedTerms:    []string{"java"},
		Source:           searchcore.SourceLocal,
		Limit:            1,
		Offset:           1,
		ContentDomain:    searchcore.ContentDomainText,
		Language:         "EN",
		SiteHost:         "example.org",
		InURL:            "example",
		TLD:              "org",
		FileType:         ".pdf",
		URLMaskFilter:    `https://.*`,
		PreferMaskFilter: `docs`,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if index.got.Query != "golang" ||
		index.got.MaxResults != 2 ||
		index.got.Language != "en" ||
		len(index.got.ExcludeTerms) != 1 ||
		len(index.got.IncludeDomain) != 1 ||
		index.got.IncludeDomain[0] != "example.org" {
		t.Fatalf("index request = %#v", index.got)
	}
	if resp.TotalResults != 3 || len(resp.Results) != 1 {
		t.Fatalf("response = %#v", resp)
	}
	result := resp.Results[0]
	if result.Title != "First" ||
		result.URL != "https://example.org/file.pdf" ||
		result.DisplayURL != "example.org/file.pdf" ||
		result.Snippet != "first snippet" ||
		result.Source != searchcore.SourceLocal ||
		result.Host != "example.org" ||
		result.File != "file.pdf" ||
		result.URLHash == "" ||
		result.ContentDomain != searchcore.ContentDomainText ||
		result.Language != "EN" ||
		result.Author != "Ada Lovelace" ||
		result.Keywords != "go, search" ||
		result.Publisher != "Example Press" {
		t.Fatalf("result = %#v", result)
	}
}

func TestSearcherUsesTermsAndDefaultLimit(t *testing.T) {
	index := &fakeIndex{}
	_, err := NewSearcher(index).Search(t.Context(), searchcore.Request{
		Terms: []string{"one", "two"},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if index.got.Query != "one two" ||
		index.got.MaxResults != searchcore.DefaultPublicLimit {
		t.Fatalf("index request = %#v", index.got)
	}
}

func TestSearcherReturnsErrors(t *testing.T) {
	sentinel := errors.New("down")
	cases := []struct {
		name     string
		searcher searchcore.Searcher
		req      searchcore.Request
	}{
		{
			name:     "nil index",
			searcher: NewSearcher(nil),
		},
		{
			name:     "index",
			searcher: NewSearcher(&fakeIndex{err: sentinel}),
			req:      searchcore.Request{Query: "golang", Limit: 1},
		},
		{
			name:     "url mask",
			searcher: NewSearcher(&fakeIndex{}),
			req:      searchcore.Request{Query: "golang", Limit: 1, URLMaskFilter: "["},
		},
		{
			name:     "prefer mask",
			searcher: NewSearcher(&fakeIndex{}),
			req:      searchcore.Request{Query: "golang", Limit: 1, PreferMaskFilter: "["},
		},
	}
	for _, tc := range cases {
		if _, err := tc.searcher.Search(t.Context(), tc.req); err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
	}
}

func TestCoreResultFallbacksAndOffset(t *testing.T) {
	resp, err := NewSearcher(&fakeIndex{response: searchindex.SearchResultSet{
		Total: 1,
		Results: []searchindex.SearchResult{{
			Title:   "Bad URL",
			URL:     "%",
			Snippet: "snippet",
			Score:   1,
		}},
	}}).Search(t.Context(), searchcore.Request{Query: "bad", Limit: 1, Offset: 2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 0 {
		t.Fatalf("results = %#v", resp.Results)
	}

	result := coreResult(
		searchcore.Request{},
		searchindex.SearchResult{Title: "Local", URL: "/local", Snippet: "snippet"},
	)
	if result.DisplayURL != "/local" || result.URLHash == "" {
		t.Fatalf("result = %#v", result)
	}
	result = coreResult(
		searchcore.Request{},
		searchindex.SearchResult{Title: "Bad URL", URL: "", Snippet: "snippet"},
	)
	if result.URLHash == "" {
		t.Fatal("empty URL still has a YaCy hash")
	}
	host, pathValue, file := parsedURLParts(nil)
	if host != "" || pathValue != "" || file != "" {
		t.Fatalf("nil parts = %q %q %q", host, pathValue, file)
	}
	_, _, file = parsedURLParts(mustParseURL(t, "https://example.org/"))
	if file != "" {
		t.Fatalf("file = %q, want empty", file)
	}
	if displayURL("", "/path") != "/path" {
		t.Fatal("displayURL without host should return path")
	}
	if got := offsetResults([]searchcore.Result{{URL: "one"}}, 0, 5); len(got) != 1 {
		t.Fatalf("offset results = %#v", got)
	}
}

func TestResultFiltersRejectEachCondition(t *testing.T) {
	filters, err := requestFilters(searchcore.Request{URLMaskFilter: "allowed"})
	if err != nil {
		t.Fatalf("requestFilters: %v", err)
	}
	if filters.match(searchcore.Request{}, searchcore.Result{URL: "https://blocked.example/"}) {
		t.Fatal("url mask should reject")
	}

	cases := []searchcore.Request{
		{InURL: "allowed"},
		{TLD: "net"},
		{FileType: "pdf"},
	}
	for _, req := range cases {
		if (resultFilters{}).match(req, searchcore.Result{
			URL:  "https://example.org/file.html",
			Host: "example.org",
			File: "file.html",
		}) {
			t.Fatalf("filter accepted request %#v", req)
		}
	}
	if !hostMatchesTLD("example.org", ".org") ||
		!fileMatchesType("FILE.PDF", "pdf") {
		t.Fatal("case-insensitive tld/filetype match failed")
	}
}

func TestSearcherLimitsResultsPerHost(t *testing.T) {
	results := []searchindex.SearchResult{
		{Title: "a1", URL: "https://a.example/1"},
		{Title: "a2", URL: "https://a.example/2"},
		{Title: "a3", URL: "https://a.example/3"},
		{Title: "a4", URL: "https://a.example/4"},
		{Title: "a5", URL: "https://a.example/5"},
		{Title: "a6", URL: "https://a.example/6"},
		{Title: "empty", URL: ""},
		{Title: "b1", URL: "https://b.example/1"},
	}
	resp, err := NewSearcher(&fakeIndex{response: searchindex.SearchResultSet{
		Total:   len(results),
		Results: results,
	}}).Search(t.Context(), searchcore.Request{Query: "golang", Limit: 20})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(resp.Results) != len(results) {
		t.Fatalf(
			"results = %d, want %d (diversity must not drop results)",
			len(resp.Results),
			len(results),
		)
	}

	fromHostA := 0
	for _, result := range resp.Results[:len(resp.Results)-1] {
		if result.Host == "a.example" {
			fromHostA++
		}
	}
	if fromHostA != 5 {
		t.Fatalf("a.example in the kept results = %d, want 5 (capped)", fromHostA)
	}
	if last := resp.Results[len(resp.Results)-1]; last.Host != "a.example" {
		t.Fatalf("last result host = %q, want the demoted a.example overflow", last.Host)
	}
}

func mustParseURL(tb testing.TB, raw string) *url.URL {
	tb.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		tb.Fatalf("url.Parse: %v", err)
	}

	return parsed
}

func TestCoreImagesConvertsRefs(t *testing.T) {
	images := coreImages([]searchindex.ResultImage{
		{URL: "https://img.example/a.png", Alt: "alpha"},
		{URL: "https://img.example/b.png", Alt: "beta"},
	})
	if len(images) != 2 {
		t.Fatalf("images = %d, want 2", len(images))
	}
	if images[0] != (searchcore.ResultImage{URL: "https://img.example/a.png", Alt: "alpha"}) {
		t.Fatalf("image[0] = %#v", images[0])
	}
	if images[1].URL != "https://img.example/b.png" || images[1].Alt != "beta" {
		t.Fatalf("image[1] = %#v", images[1])
	}
}

func TestSearcherThreadsProviderWeights(t *testing.T) {
	index := &fakeIndex{}
	want := searchindex.RankingWeights{Title: 7, Headings: 5, Anchors: 3, Body: 2, URL: 1}
	searcher := NewSearcherWithWeights(
		index,
		func() searchindex.RankingWeights { return want },
	)
	if _, err := searcher.Search(t.Context(), searchcore.Request{Query: "golang"}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if index.got.Weights != want {
		t.Fatalf("index weights = %+v, want %+v", index.got.Weights, want)
	}
}

func TestSearcherDefaultsWeightsWithoutProvider(t *testing.T) {
	index := &fakeIndex{}
	if _, err := NewSearcher(index).Search(
		t.Context(),
		searchcore.Request{Query: "golang"},
	); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if index.got.Weights != (searchindex.RankingWeights{}) {
		t.Fatalf("index weights = %+v, want zero", index.got.Weights)
	}
}
