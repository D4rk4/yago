package searchindex

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestBleveDiskSearchEvidenceLifecycle(t *testing.T) {
	doc := documentstore.Document{
		NormalizedURL: "https://example.org/evidence",
		Title:         "Evidence",
		ExtractedText: strings.Repeat("leading material ", 30) + "needle context",
		Language:      "en",
	}
	directory := newFakeDocumentDirectory(doc)
	index, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "search.bleve"),
		directory,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBleveDiskIndex: %v", err)
	}
	if err := index.Index(t.Context(), doc); err != nil {
		t.Fatalf("Index: %v", err)
	}
	candidate := SearchResult{
		DocumentID: documentID(doc),
		Snippet:    "candidate",
		Score:      1,
		Analyzer:   searchTextAnalyzer,
	}
	enriched, err := index.SearchEvidence(
		t.Context(),
		SearchRequest{Query: "needle", Terms: []string{"needle"}},
		[]SearchResult{candidate},
	)
	if err != nil {
		t.Fatalf("SearchEvidence: %v", err)
	}
	if len(enriched) != 1 || !strings.Contains(enriched[0].Snippet, "needle context") {
		t.Fatalf("enriched = %#v", enriched)
	}

	sentinel := errors.New("document directory unavailable")
	directory.err = sentinel
	if _, err := index.SearchEvidence(
		t.Context(),
		SearchRequest{Query: "needle"},
		[]SearchResult{candidate},
	); !errors.Is(err, sentinel) {
		t.Fatalf("directory error = %v", err)
	}
	directory.err = nil
	delete(directory.documents, candidate.DocumentID)
	enriched, err = index.SearchEvidence(
		t.Context(),
		SearchRequest{Query: "needle"},
		[]SearchResult{candidate},
	)
	if err != nil || len(enriched) != 0 {
		t.Fatalf("orphan evidence=%#v error=%v", enriched, err)
	}
	stats, err := index.Stats(t.Context())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Documents != 0 {
		t.Fatalf("orphaned documents = %d", stats.Documents)
	}
	if err := index.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := index.SearchEvidence(
		t.Context(),
		SearchRequest{Query: "needle"},
		[]SearchResult{candidate},
	); err == nil {
		t.Fatal("closed index accepted evidence search")
	}
}

func TestSearchEvidencePreservesCandidatesWhenDocumentLoadIsCanceled(t *testing.T) {
	results := []SearchResult{{DocumentID: "one"}, {DocumentID: "two"}}
	enriched, err := searchEvidenceResults(
		t.Context(),
		SearchRequest{Query: "needle"},
		results,
		func(int) (documentstore.Document, bool, error) {
			return documentstore.Document{}, false, context.Canceled
		},
	)
	if err != nil || len(enriched) != len(results) || enriched[1].DocumentID != "two" {
		t.Fatalf("results=%#v error=%v", enriched, err)
	}
}

func TestSearchEvidenceRescoresCompletePositionalWindow(t *testing.T) {
	results := []SearchResult{
		{DocumentID: "far", Score: 1, Analyzer: searchTextAnalyzer},
		{DocumentID: "near", Score: 1, Analyzer: searchTextAnalyzer},
	}
	documents := []documentstore.Document{
		{NormalizedURL: "far", ExtractedText: "alpha filler filler filler filler beta"},
		{NormalizedURL: "near", ExtractedText: "alpha beta"},
	}
	enriched, err := searchEvidenceResults(
		t.Context(),
		SearchRequest{
			Query: "alpha beta", Terms: []string{"alpha", "beta"}, IncludePositions: true,
		},
		results,
		func(index int) (documentstore.Document, bool, error) {
			return documents[index], true, nil
		},
	)
	if err != nil {
		t.Fatalf("searchEvidenceResults: %v", err)
	}
	if len(enriched) != 2 || enriched[0].DocumentID != "near" ||
		enriched[0].Score <= enriched[1].Score {
		t.Fatalf("rescored = %#v", enriched)
	}
}

func TestStoredEvidenceResultPropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, found, err := searchEvidenceResult(
		ctx,
		SearchRequest{Query: "needle", Terms: []string{"needle"}},
		SearchResult{DocumentID: "one"},
		0,
		func(int) (documentstore.Document, bool, error) {
			return documentstore.Document{ExtractedText: "needle"}, true, nil
		},
	)
	if found || !errors.Is(err, context.Canceled) {
		t.Fatalf("found=%t error=%v", found, err)
	}
}
