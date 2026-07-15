package searchindex

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestArabicAnalyzerVariantProximityEndToEnd(t *testing.T) {
	queryTerms := []string{"الحسن", "زوجها"}
	documents := []documentstore.Document{
		{
			NormalizedURL: "https://alpha.example/scattered",
			ExtractedText: "والحسن 1 2 3 4 5 6 7 8 9 زوج",
			Language:      "ar",
		},
		{
			NormalizedURL: "https://middle.example/variant",
			ExtractedText: "والحسن زوج",
			Language:      "ar",
		},
		{
			NormalizedURL: "https://zebra.example/exact",
			ExtractedText: "الحسن زوجها",
			Language:      "ar",
		},
	}
	results := searchLanguageProximityDocuments(t, queryTerms, documents)
	if len(results) != len(documents) {
		t.Fatalf("Arabic result count = %d, want %d: %#v", len(results), len(documents), results)
	}
	if results[0].URL != documents[2].NormalizedURL ||
		results[1].URL != documents[1].NormalizedURL ||
		results[2].URL != documents[0].NormalizedURL {
		t.Fatalf("Arabic result order = %#v", results)
	}
	if results[0].Analyzer != "ar" || results[1].Analyzer != "ar" {
		t.Fatalf("Arabic analyzers = %q/%q", results[0].Analyzer, results[1].Analyzer)
	}
	if results[0].Proximity != 1 || results[0].OrderedProximity != 1 ||
		results[1].Proximity != analyzerVariantPairConfidence ||
		results[1].OrderedProximity != analyzerVariantPairConfidence ||
		results[2].Proximity != 0 || results[2].OrderedProximity != 0 {
		t.Fatalf("Arabic proximity = %#v", results)
	}
	for _, term := range queryTerms {
		if len(results[0].FieldTermPositions["body"][term]) == 0 {
			t.Fatalf("Arabic exact position %q missing: %#v", term, results[0].FieldTermPositions)
		}
		if len(results[1].FieldTermPositions["body"][term]) != 0 {
			t.Fatalf(
				"Arabic analyzer variant exposed as exact %q: %#v",
				term,
				results[1].FieldTermPositions,
			)
		}
	}
}

func TestArabicAnalyzerNormalizationKeepsStoredEvidence(t *testing.T) {
	queryTerms := []string{"أحمد", "يوسف"}
	documents := []documentstore.Document{
		{
			NormalizedURL: "https://example.test/arabic-normalization",
			ExtractedText: "أحمد يوسف",
			Language:      "ar",
		},
	}
	results := searchLanguageProximityDocuments(t, queryTerms, documents)
	if len(results) != 1 || results[0].Proximity != 1 || results[0].OrderedProximity != 1 {
		t.Fatalf("Arabic normalized evidence = %#v", results)
	}
	for _, term := range queryTerms {
		if len(results[0].FieldTermPositions["body"][term]) != 1 {
			t.Fatalf("Arabic normalized position %q = %#v", term, results[0].FieldTermPositions)
		}
	}
}

func TestHebrewExactProximityEndToEnd(t *testing.T) {
	queryTerms := []string{"מערכת", "חיפוש"}
	documents := []documentstore.Document{
		{
			NormalizedURL: "https://alpha.example/scattered",
			ExtractedText: "מערכת 1 2 3 4 5 6 7 8 9 חיפוש",
			Language:      "he",
		},
		{
			NormalizedURL: "https://zebra.example/compact",
			ExtractedText: "מערכת חיפוש",
			Language:      "he",
		},
	}
	results := searchLanguageProximityDocuments(t, queryTerms, documents)
	if len(results) != len(documents) {
		t.Fatalf("Hebrew result count = %d, want %d: %#v", len(results), len(documents), results)
	}
	if results[0].URL != documents[1].NormalizedURL ||
		results[1].URL != documents[0].NormalizedURL {
		t.Fatalf("Hebrew result order = %#v", results)
	}
	if results[0].Analyzer != standardTextAnalyzer ||
		results[1].Analyzer != standardTextAnalyzer {
		t.Fatalf("Hebrew analyzers = %q/%q", results[0].Analyzer, results[1].Analyzer)
	}
	if results[0].Proximity != 1 || results[0].OrderedProximity != 1 ||
		results[1].Proximity != 0 || results[1].OrderedProximity != 0 {
		t.Fatalf("Hebrew proximity = %#v", results)
	}
}

func TestHebrewRecallDoesNotClaimMorphology(t *testing.T) {
	queryTerms := []string{"מערכת", "חיפוש"}
	documents := []documentstore.Document{
		{
			NormalizedURL: "https://alpha.example/inflected",
			ExtractedText: "מערכות חיפושים",
			Language:      "he",
		},
		{
			NormalizedURL: "https://zebra.example/exact",
			ExtractedText: "מערכת חיפוש",
			Language:      "he",
		},
	}
	results := searchLanguageProximityDocuments(t, queryTerms, documents)
	if len(results) != 1 || results[0].URL != documents[1].NormalizedURL {
		t.Fatalf("Hebrew exact-only recall = %#v", results)
	}
}

func searchLanguageProximityDocuments(
	t *testing.T,
	queryTerms []string,
	documents []documentstore.Document,
) []SearchResult {
	t.Helper()
	index, err := NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	for _, document := range documents {
		if err := index.Index(t.Context(), document); err != nil {
			t.Fatalf("Index(%s): %v", document.NormalizedURL, err)
		}
	}
	result, err := index.Search(t.Context(), SearchRequest{
		Query:            queryTerms[0] + " " + queryTerms[1],
		Terms:            queryTerms,
		MaxResults:       len(documents),
		IncludePositions: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	return result.Results
}
