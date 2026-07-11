package searchindex

import (
	"reflect"
	"testing"

	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestSearchResultUsesStoredContentQualityEvidence(t *testing.T) {
	evidence := documentstore.ContentQualityEvidence{
		Known:                true,
		Score:                0.25,
		FunctionWordFraction: 0.2,
		SymbolFraction:       0.01,
		AlphabeticFraction:   0.9,
		UniqueTokenFraction:  0.7,
		SpamRisk:             0.375,
	}
	result := searchResultFromDocument(
		&search.DocumentMatch{ID: "document", Score: 2},
		documentstore.Document{
			NormalizedURL:  "https://example.org/",
			Title:          "Example",
			ExtractedText:  "short text that must not be rescanned",
			ContentQuality: evidence,
		},
		SearchRequest{},
	)
	got := documentstore.ContentQualityEvidence{
		Known:                result.QualityKnown,
		Score:                result.Quality,
		FunctionWordFraction: result.FunctionWordFraction,
		SymbolFraction:       result.SymbolFraction,
		AlphabeticFraction:   result.AlphabeticFraction,
		UniqueTokenFraction:  result.UniqueTokenFraction,
		SpamRisk:             result.SpamRisk,
	}
	if !reflect.DeepEqual(got, evidence) {
		t.Fatalf("result evidence = %#v, want %#v", got, evidence)
	}
}

func TestSearchResultKeepsLegacyContentQualityNeutral(t *testing.T) {
	result := searchResultFromDocument(
		&search.DocumentMatch{ID: "legacy"},
		documentstore.Document{
			NormalizedURL: "https://example.org/legacy",
			ExtractedText: "the cat and dog are in the house and sun bright alpha beta gamma delta epsilon zeta eta theta iota",
		},
		SearchRequest{},
	)
	if result.QualityKnown || result.Quality != 0 || result.SpamRisk != 0 ||
		result.FunctionWordFraction != 0 || result.SymbolFraction != 0 ||
		result.AlphabeticFraction != 0 || result.UniqueTokenFraction != 0 {
		t.Fatalf("legacy result evidence = %#v", result)
	}
}
