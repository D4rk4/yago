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

	response := i.response
	if len(response.Results) > req.MaxResults {
		response.Results = append(
			[]searchindex.SearchResult(nil),
			response.Results[:req.MaxResults]...,
		)
	}

	return response, nil
}

func (i *fakeIndex) Stats(context.Context) (searchindex.IndexStats, error) {
	return searchindex.IndexStats{}, nil
}

func TestSearcherTranslatesAndFiltersResults(t *testing.T) {
	published := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	index := translatedSearchIndex(published)

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
		SafeSearch:       true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if index.got.Query != "golang" ||
		index.got.MaxResults != 2 ||
		index.got.Language != "en" ||
		len(index.got.ExcludeTerms) != 1 ||
		len(index.got.IncludeDomain) != 1 ||
		index.got.IncludeDomain[0] != "example.org" || !index.got.SafeSearch {
		t.Fatalf("index request = %#v", index.got)
	}
	if resp.TotalResults != 3 || len(resp.Results) != 1 {
		t.Fatalf("response = %#v", resp)
	}
	assertTranslatedResult(t, resp.Results[0])
}

func translatedSearchIndex(published time.Time) *fakeIndex {
	return &fakeIndex{response: searchindex.SearchResultSet{
		Total: 3,
		Results: []searchindex.SearchResult{
			{
				Title:          "Second",
				URL:            "https://docs.example.org/guide.pdf",
				Snippet:        "second snippet",
				Score:          2,
				PublishedDate:  published,
				DateConfidence: 0.8,
			},
			{
				Title:               "First",
				URL:                 "https://example.org/file.pdf",
				Snippet:             "first snippet",
				Score:               1,
				Author:              "Ada Lovelace",
				Keywords:            "go, search",
				Publisher:           "Example Press",
				Language:            "ru",
				SafetyRating:        documentstore.SafetyGeneral,
				ExplicitProbability: 0.1,
				SafetyConfidence:    0.8,
			},
			{
				Title:   "Rejected",
				URL:     "https://example.net/file.txt",
				Snippet: "rejected",
				Score:   3,
			},
		},
	}}
}

func assertTranslatedResult(t *testing.T, result searchcore.Result) {
	t.Helper()
	if result.Title != "First" ||
		result.URL != "https://example.org/file.pdf" ||
		result.DisplayURL != "example.org/file.pdf" ||
		result.Snippet != "first snippet" ||
		result.Source != searchcore.SourceLocal ||
		result.Host != "example.org" ||
		result.File != "file.pdf" ||
		result.URLHash == "" ||
		result.ContentDomain != searchcore.ContentDomainText ||
		result.Language != "ru" ||
		result.SafetyRating != searchcore.SafetyGeneral ||
		result.ExplicitProbability != 0.1 || result.SafetyConfidence != 0.8 ||
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

func TestSearcherReranksBoundedCandidateWindow(t *testing.T) {
	index := &fakeIndex{response: searchindex.SearchResultSet{
		Total: 2,
		Results: []searchindex.SearchResult{
			{Title: "Retrieved first", URL: "https://a.example/deep/page", Score: 1},
			{
				Title:        "Prior winner",
				URL:          "https://b.example/deep/page",
				Score:        0.8,
				Quality:      1,
				QualityKnown: true,
			},
		},
	}}
	searcher := searchcore.NewLexicalRerankSearcher(NewSearcherWithRanking(
		index,
		func() searchindex.RankingWeights {
			return searchindex.RankingWeights{Title: 1, Quality: 1}
		},
		nil,
	))
	resp, err := searcher.Search(t.Context(), searchcore.Request{Query: "rank", Limit: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if index.got.MaxResults != 50 {
		t.Fatalf("MaxResults = %d, want 50", index.got.MaxResults)
	}
	if len(resp.Results) != 1 || resp.Results[0].Title != "Prior winner" {
		t.Fatalf("results = %+v", resp.Results)
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
		searchindex.SearchResult{
			Title: "Local", URL: "/local", Snippet: "snippet", DateConfidence: 0.8,
			Quality: 0.4, QualityKnown: true, SpamRisk: 0.3,
			FunctionWordFraction: 0.2, SymbolFraction: 0.1,
			AlphabeticFraction: 0.8, UniqueTokenFraction: 0.7,
		},
	)
	if result.DisplayURL != "/local" || result.URLHash == "" || result.DateConfidence != 0.8 ||
		result.Date != "" ||
		result.Quality != 0.4 || !result.QualityKnown || result.SpamRisk != 0.3 ||
		result.FunctionWordFraction != 0.2 || result.SymbolFraction != 0.1 ||
		result.AlphabeticFraction != 0.8 || result.UniqueTokenFraction != 0.7 {
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

func TestResultFiltersURLMask(t *testing.T) {
	filters, err := requestFilters(searchcore.Request{URLMaskFilter: "allowed"})
	if err != nil {
		t.Fatalf("requestFilters: %v", err)
	}
	if filters.match(searchcore.Result{URL: "https://blocked.example/"}) {
		t.Fatal("url mask should reject a non-matching url")
	}
	if !filters.match(searchcore.Result{URL: "https://allowed.example/"}) {
		t.Fatal("url mask should accept a matching url")
	}
	if !(resultFilters{}).match(searchcore.Result{URL: "https://anything.example/"}) {
		t.Fatal("empty filters should accept every url")
	}
}

func TestSearcherLeavesHostCrowdingToFinalRankingStage(t *testing.T) {
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
	indexResult := searchindex.SearchResultSet{
		Total:   len(results),
		Results: results,
	}
	resp, err := NewSearcher(&fakeIndex{response: indexResult}).Search(
		t.Context(),
		searchcore.Request{Query: "golang", Limit: 20},
	)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(resp.Results) != len(results) || resp.Results[5].Host != "a.example" ||
		resp.Results[7].Host != "b.example" {
		t.Fatalf("retrieval order = %+v", resp.Results)
	}

	final, err := searchcore.NewLexicalRerankSearcher(
		NewSearcher(&fakeIndex{response: indexResult}),
	).Search(t.Context(), searchcore.Request{Query: "golang", Limit: 20})
	if err != nil {
		t.Fatalf("final search: %v", err)
	}
	if len(final.Results) != len(results) || final.Results[3].Host != "b.example" ||
		final.Results[4].Host != "a.example" {
		t.Fatalf("final order = %+v", final.Results)
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
