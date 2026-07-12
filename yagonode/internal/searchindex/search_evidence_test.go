package searchindex

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestSearchEvidenceIsBoundedAndRestoresRawContent(t *testing.T) {
	body := strings.Repeat("unrelated filler text ", 40) + "needle evidence"
	results := make([]SearchResult, maximumSearchEvidenceResults+5)
	for index := range results {
		results[index] = SearchResult{
			DocumentID: fmt.Sprintf("doc-%02d", index),
			Snippet:    "candidate snippet",
			Score:      float64(len(results) - index),
			Analyzer:   searchTextAnalyzer,
		}
	}
	loads := 0
	enriched, err := searchEvidenceResults(
		t.Context(),
		SearchRequest{
			Query: "needle", Terms: []string{"needle"}, IncludeRaw: true,
		},
		results,
		func(int) (documentstore.Document, bool, error) {
			loads++
			return documentstore.Document{ExtractedText: body}, true, nil
		},
	)
	if err != nil {
		t.Fatalf("searchEvidenceResults: %v", err)
	}
	if loads != maximumSearchEvidenceResults || len(enriched) != len(results) {
		t.Fatalf("loads=%d results=%d", loads, len(enriched))
	}
	if !strings.Contains(enriched[0].Snippet, "needle evidence") ||
		enriched[0].RawContent != body {
		t.Fatalf("enriched result = %#v", enriched[0])
	}
	if enriched[maximumSearchEvidenceResults].Snippet != "candidate snippet" ||
		enriched[maximumSearchEvidenceResults].RawContent != "" {
		t.Fatalf("bounded tail = %#v", enriched[maximumSearchEvidenceResults])
	}
}

func TestSearchEvidenceKeepsTailAfterAMissingDocument(t *testing.T) {
	results := make([]SearchResult, maximumSearchEvidenceResults+1)
	for index := range results {
		results[index] = SearchResult{DocumentID: fmt.Sprintf("doc-%02d", index)}
	}
	enriched, err := searchEvidenceResults(
		t.Context(),
		SearchRequest{Query: "needle", Terms: []string{"needle"}},
		results,
		func(index int) (documentstore.Document, bool, error) {
			return documentstore.Document{ExtractedText: "needle"}, index != 0, nil
		},
	)
	if err != nil {
		t.Fatalf("searchEvidenceResults: %v", err)
	}
	if len(enriched) != len(results)-1 ||
		enriched[len(enriched)-1].DocumentID != results[len(results)-1].DocumentID {
		t.Fatalf("results = %#v", enriched)
	}
}

func TestSearchEvidencePreservesCandidatesAtDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	results := []SearchResult{{DocumentID: "one"}, {DocumentID: "two"}}
	enriched, err := searchEvidenceResults(
		ctx,
		SearchRequest{Query: "needle"},
		results,
		func(int) (documentstore.Document, bool, error) {
			t.Fatal("document load after cancellation")
			return documentstore.Document{}, false, nil
		},
	)
	if err != nil || len(enriched) != len(results) || enriched[0].DocumentID != "one" {
		t.Fatalf("results=%#v error=%v", enriched, err)
	}
}

func TestSearchEvidenceReturnsDocumentErrors(t *testing.T) {
	sentinel := errors.New("read failed")
	_, err := searchEvidenceResults(
		t.Context(),
		SearchRequest{Query: "needle"},
		[]SearchResult{{DocumentID: "one"}},
		func(int) (documentstore.Document, bool, error) {
			return documentstore.Document{}, false, sentinel
		},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v", err)
	}
}

func TestCandidateSearchDefersRawContent(t *testing.T) {
	doc := documentstore.Document{
		NormalizedURL: "https://example.org/needle",
		Title:         "Needle",
		ExtractedText: strings.Repeat("leading text ", 40) + "needle evidence",
		Language:      "en",
	}
	index, err := NewBleveMemoryIndex(
		t.Context(),
		&fakeStoredDocuments{documents: []documentstore.Document{doc}},
	)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	candidates, err := index.Search(t.Context(), SearchRequest{
		Query: "needle", Terms: []string{"needle"}, MaxResults: 1,
		IncludeRaw: true, CandidateOnly: true,
	})
	if err != nil {
		t.Fatalf("candidate Search: %v", err)
	}
	if len(candidates.Results) != 1 || candidates.Results[0].RawContent != "" ||
		strings.Contains(candidates.Results[0].Snippet, "needle evidence") {
		t.Fatalf("candidate = %#v", candidates.Results)
	}
	enriched, err := index.SearchEvidence(
		t.Context(),
		SearchRequest{
			Query: "needle", Terms: []string{"needle"}, IncludeRaw: true,
		},
		candidates.Results,
	)
	if err != nil {
		t.Fatalf("SearchEvidence: %v", err)
	}
	if len(enriched) != 1 || enriched[0].RawContent != doc.ExtractedText ||
		!strings.Contains(enriched[0].Snippet, "needle evidence") {
		t.Fatalf("enriched = %#v", enriched)
	}
}
