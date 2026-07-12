package searchindex

import (
	"fmt"
	"testing"

	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestStoredPhraseEvidencePrefersAdjacentEqualScoreCandidate(t *testing.T) {
	candidates := []SearchResult{
		{DocumentID: "scattered", Score: 1, Analyzer: searchTextAnalyzer},
		{DocumentID: "adjacent", Score: 1, Analyzer: searchTextAnalyzer},
	}
	documents := []documentstore.Document{
		{ExtractedText: "brown fox quick lazy", Language: "en"},
		{ExtractedText: "quick brown fox lazy", Language: "en"},
	}
	quoted, err := searchEvidenceResults(
		t.Context(),
		SearchRequest{
			Query: "quick brown", Terms: []string{"quick", "brown"},
			Phrases: []string{"quick brown"},
		},
		candidates,
		func(index int) (documentstore.Document, bool, error) {
			return documents[index], true, nil
		},
	)
	if err != nil {
		t.Fatalf("searchEvidenceResults: %v", err)
	}
	if quoted[0].DocumentID != "adjacent" || quoted[0].Score <= quoted[1].Score {
		t.Fatalf("quoted results = %#v", quoted)
	}

	unquoted, err := searchEvidenceResults(
		t.Context(),
		SearchRequest{Query: "quick brown", Terms: []string{"quick", "brown"}},
		candidates,
		func(index int) (documentstore.Document, bool, error) {
			return documents[index], true, nil
		},
	)
	if err != nil {
		t.Fatalf("unquoted searchEvidenceResults: %v", err)
	}
	if unquoted[0].DocumentID != "scattered" || unquoted[0].Score != unquoted[1].Score {
		t.Fatalf("unquoted results = %#v", unquoted)
	}
}

func TestStoredPhraseEvidenceUsesCandidateRussianAnalyzer(t *testing.T) {
	candidates := []SearchResult{
		{DocumentID: "scattered", Score: 1, Analyzer: "ru"},
		{DocumentID: "adjacent", Score: 1, Analyzer: "ru"},
	}
	documents := []documentstore.Document{
		{ExtractedText: "красивый старый дом", Language: "ru"},
		{ExtractedText: "красивый дом стоит", Language: "ru"},
	}
	results, err := searchEvidenceResults(
		t.Context(),
		SearchRequest{
			Query: "красивые дома", Terms: []string{"красивые", "дома"},
			Phrases: []string{"красивые дома"},
		},
		candidates,
		func(index int) (documentstore.Document, bool, error) {
			return documents[index], true, nil
		},
	)
	if err != nil {
		t.Fatalf("searchEvidenceResults: %v", err)
	}
	if results[0].DocumentID != "adjacent" || results[0].Score <= results[1].Score {
		t.Fatalf("Russian phrase results = %#v", results)
	}
}

func TestStoredPhraseEvidenceKeepsUnvalidatedTailOrder(t *testing.T) {
	candidates := make([]SearchResult, maximumSearchEvidenceResults+2)
	for index := range candidates {
		candidates[index] = SearchResult{
			DocumentID: fmt.Sprintf("doc-%02d", index),
			Score:      1,
			Analyzer:   searchTextAnalyzer,
		}
	}
	results, err := searchEvidenceResults(
		t.Context(),
		SearchRequest{
			Query: "quick brown", Terms: []string{"quick", "brown"},
			Phrases: []string{"quick brown"},
		},
		candidates,
		func(index int) (documentstore.Document, bool, error) {
			if index >= maximumSearchEvidenceResults {
				t.Fatalf("loaded unvalidated tail index %d", index)
			}
			body := "brown fox quick lazy"
			if index == maximumSearchEvidenceResults-1 {
				body = "quick brown fox lazy"
			}
			return documentstore.Document{ExtractedText: body, Language: "en"}, true, nil
		},
	)
	if err != nil {
		t.Fatalf("searchEvidenceResults: %v", err)
	}
	if results[0].DocumentID != "doc-09" {
		t.Fatalf("validated prefix = %#v", results[:maximumSearchEvidenceResults])
	}
	if results[maximumSearchEvidenceResults].DocumentID != "doc-10" ||
		results[maximumSearchEvidenceResults+1].DocumentID != "doc-11" ||
		results[maximumSearchEvidenceResults].Score != 1 ||
		results[maximumSearchEvidenceResults+1].Score != 1 {
		t.Fatalf("tail = %#v", results[maximumSearchEvidenceResults:])
	}
}

func TestStoredPhraseEvidenceFallsBackToFoldedWords(t *testing.T) {
	locations := search.FieldTermLocationMap{
		"body": {
			"quick": {{Pos: 4}},
			"brown": {{Pos: 5}},
		},
	}
	preference := storedQuotedPhrasePreference(
		locations,
		[]string{"Quick Brown", "single"},
		nil,
	)
	if preference != 1 {
		t.Fatalf("fallback phrase preference = %v", preference)
	}
}
