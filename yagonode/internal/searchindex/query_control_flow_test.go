package searchindex

import "testing"

func TestUnscopedFuzzyRecoveryFindsBoundedTypo(t *testing.T) {
	index := newRequiredTermsFixture(t)
	index.analyzerScope = false

	result, err := index.Search(t.Context(), SearchRequest{
		Query:      "черногори",
		MaxResults: 10,
		Fuzzy:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 2 {
		t.Fatalf("unscoped fuzzy total = %d, want 2", result.Total)
	}
}

func TestUnscopedAllStopwordQueryReturnsNoDocuments(t *testing.T) {
	index := newRequiredTermsFixture(t)
	index.analyzerScope = false

	result, err := index.Search(t.Context(), SearchRequest{
		Query:      "в и на",
		MaxResults: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 0 {
		t.Fatalf("all-stopword total = %d, want 0", result.Total)
	}
}

func TestUnscopedExpansionOnlyReordersRequiredMatches(t *testing.T) {
	index := newRequiredTermsFixture(t)
	index.analyzerScope = false

	result, err := index.Search(t.Context(), SearchRequest{
		Query:          "черногория",
		ExpansionTerms: []string{"интернет", "провайдер"},
		MaxResults:     10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 2 || result.Results[0].URL != "https://example.org/mn-isp" {
		t.Fatalf("unscoped expansion results = %#v", result.Results)
	}
}

func TestUnknownAnalyzerKeepsRequiredTerm(t *testing.T) {
	terms := requirableTermsForAnalyzer([]string{"needle"}, "missing-analyzer")
	if len(terms) != 1 || terms[0] != "needle" {
		t.Fatalf("required terms = %#v", terms)
	}
}
