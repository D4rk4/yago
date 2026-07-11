package searchindex

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestMinimumTermsQueryRecoversPartialMatches(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	for _, doc := range []documentstore.Document{
		{NormalizedURL: "https://example.org/all", ExtractedText: "alpha beta gamma"},
		{NormalizedURL: "https://example.org/two", ExtractedText: "alpha beta"},
		{NormalizedURL: "https://example.org/one", ExtractedText: "alpha"},
	} {
		if err := index.Index(t.Context(), doc); err != nil {
			t.Fatalf("Index: %v", err)
		}
	}
	req := SearchRequest{
		Query: "alpha beta gamma", Terms: []string{"alpha", "beta", "gamma"}, MaxResults: 10,
	}
	strict, err := index.Search(t.Context(), req)
	if err != nil {
		t.Fatalf("strict Search: %v", err)
	}
	if len(strict.Results) != 1 || strict.Results[0].URL != "https://example.org/all" {
		t.Fatalf("strict results = %#v", strict.Results)
	}
	req.MinimumTermMatches = 2
	relaxed, err := index.Search(t.Context(), req)
	if err != nil {
		t.Fatalf("relaxed Search: %v", err)
	}
	if len(relaxed.Results) != 2 || relaxed.Results[0].URL != "https://example.org/all" {
		t.Fatalf("relaxed results = %#v", relaxed.Results)
	}
}

func TestMinimumTermsQueryHandlesFallbackAndExpansion(t *testing.T) {
	weights := DefaultRankingWeights()
	if query := minimumTermsQuery(SearchRequest{
		Query: "alpha", MinimumTermMatches: 3,
	}, []string{""}, weights); query == nil {
		t.Fatal("fallback query is nil")
	}
	if query := minimumTermsQuery(SearchRequest{
		Query: "alpha beta", Terms: []string{"alpha", "beta"},
		MinimumTermMatches: 1, ExpansionTerms: []string{"gamma"},
	}, []string{""}, weights); query == nil {
		t.Fatal("expanded query is nil")
	}
	if query := minimumTermsQuery(SearchRequest{
		Query: "the", Terms: []string{"the"}, MinimumTermMatches: 1,
	}, []string{"en"}, weights); query == nil {
		t.Fatal("analyzed-away fallback query is nil")
	}
}
