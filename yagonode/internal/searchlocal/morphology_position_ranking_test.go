package searchlocal

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestEnglishAnalyzerPositionsRankCompactStrictDocument(t *testing.T) {
	index, err := searchindex.NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	documents := []documentstore.Document{
		{
			NormalizedURL: "https://example.org/compact",
			Title:         "Compact",
			ExtractedText: "gaming mouse alpha beta gamma delta",
			Language:      "en",
		},
		{
			NormalizedURL: "https://example.org/scattered",
			Title:         "Scattered",
			ExtractedText: "gaming alpha beta gamma delta mouse",
			Language:      "en",
		},
		{
			NormalizedURL: "https://example.org/reversed",
			Title:         "Reversed",
			ExtractedText: "mouse alpha beta gamma delta gaming",
			Language:      "en",
		},
	}
	for _, document := range documents {
		if err := index.Index(t.Context(), document); err != nil {
			t.Fatalf("Index(%s): %v", document.NormalizedURL, err)
		}
	}
	request := searchcore.Request{
		Query: "gaming mouse",
		Terms: []string{"gaming", "mouse"},
		Limit: 3,
	}
	response, err := searchcore.NewLexicalEvidenceSearcher(NewSearcher(index)).Search(
		t.Context(),
		request,
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(response.Results) != 3 || response.Results[0].URL != documents[0].NormalizedURL {
		t.Fatalf("ranked results = %#v", response.Results)
	}
	for _, result := range response.Results {
		positions := result.FieldTermPositions["body"]
		if len(positions["gaming"]) == 0 || len(positions["mouse"]) == 0 {
			t.Fatalf("raw positions for %s = %#v", result.URL, positions)
		}
		if _, exposed := positions["game"]; exposed {
			t.Fatalf("stem identity exposed for %s: %#v", result.URL, positions)
		}
	}
}

func TestRelaxedSearchRejectsAnalyzerOnlySurfaceEvidence(t *testing.T) {
	index, err := searchindex.NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	documents := []documentstore.Document{
		{
			NormalizedURL: "https://example.org/analyzer-only",
			Title:         "Analyzer only",
			ExtractedText: "best game changer",
			Language:      "en",
		},
		{
			NormalizedURL: "https://example.org/relevant",
			Title:         "Relevant",
			ExtractedText: "best mouse games",
			Language:      "en",
		},
	}
	for _, document := range documents {
		if err := index.Index(t.Context(), document); err != nil {
			t.Fatalf("Index(%s): %v", document.NormalizedURL, err)
		}
	}
	response, err := NewSearcher(index).Search(t.Context(), searchcore.Request{
		Query: "best mouse gaming",
		Terms: []string{"best", "mouse", "gaming"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(response.Results) != 1 || response.Results[0].URL != documents[1].NormalizedURL {
		t.Fatalf("results = %#v", response.Results)
	}
}
