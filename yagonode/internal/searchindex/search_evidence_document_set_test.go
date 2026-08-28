package searchindex

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type evidenceDocumentSetDirectory struct {
	documents       map[string]documentstore.Document
	requested       []string
	setReads        int
	individualReads int
	failure         error
	malformed       bool
}

func (d *evidenceDocumentSetDirectory) Document(
	_ context.Context,
	normalizedURL string,
) (documentstore.Document, bool, error) {
	d.individualReads++
	document, found := d.documents[normalizedURL]

	return document, found, d.failure
}

func (d *evidenceDocumentSetDirectory) Documents(
	_ context.Context,
	normalizedURLs []string,
) ([]documentstore.Document, []bool, error) {
	d.setReads++
	d.requested = append([]string(nil), normalizedURLs...)
	if d.failure != nil {
		return nil, nil, d.failure
	}
	documents := make([]documentstore.Document, len(normalizedURLs))
	found := make([]bool, len(normalizedURLs))
	for index, normalizedURL := range normalizedURLs {
		documents[index], found[index] = d.documents[normalizedURL]
	}
	if d.malformed {
		documents = documents[:len(documents)-1]
	}

	return documents, found, nil
}

func (d *evidenceDocumentSetDirectory) Count(context.Context) (int, error) {
	return len(d.documents), d.failure
}

func TestSearchEvidenceReadsVisibleDocumentsAsOneSet(t *testing.T) {
	results := make([]SearchResult, maximumSearchEvidenceResults+2)
	documents := make(map[string]documentstore.Document, maximumSearchEvidenceResults)
	for index := range results {
		identity := fmt.Sprintf("https://example.org/%02d", index)
		results[index] = SearchResult{
			DocumentID: identity,
			Snippet:    "candidate",
			Score:      float64(len(results) - index),
			Analyzer:   searchTextAnalyzer,
		}
		if index < maximumSearchEvidenceResults {
			documents[identity] = documentstore.Document{
				NormalizedURL: identity,
				ExtractedText: strings.Repeat("leading material ", 20) + "needle evidence",
			}
		}
	}
	directory := &evidenceDocumentSetDirectory{documents: documents}
	index := &BleveDiskIndex{documents: directory}
	enriched, err := index.SearchEvidence(
		t.Context(),
		SearchRequest{Query: "needle", Terms: []string{"needle"}},
		results,
	)
	if err != nil {
		t.Fatal(err)
	}
	if directory.setReads != 1 || directory.individualReads != 0 ||
		len(directory.requested) != maximumSearchEvidenceResults {
		t.Fatalf(
			"set/individual/requested=%d/%d/%d",
			directory.setReads,
			directory.individualReads,
			len(directory.requested),
		)
	}
	for index, requested := range directory.requested {
		if requested != results[index].DocumentID {
			t.Fatalf("requested[%d]=%q, want=%q", index, requested, results[index].DocumentID)
		}
	}
	if len(enriched) != len(results) ||
		!strings.Contains(enriched[0].Snippet, "needle evidence") ||
		enriched[len(enriched)-1].Snippet != "candidate" {
		t.Fatalf("enriched=%#v", enriched)
	}
}

func TestSearchEvidenceDocumentSetSkipsEmptyInput(t *testing.T) {
	directory := &evidenceDocumentSetDirectory{}
	index := &BleveDiskIndex{documents: directory}
	enriched, err := index.SearchEvidence(t.Context(), SearchRequest{}, nil)
	if err != nil || len(enriched) != 0 || directory.setReads != 0 {
		t.Fatalf("enriched=%v reads=%d error=%v", enriched, directory.setReads, err)
	}
}

func TestSearchEvidenceDocumentSetKeepsEvidenceAfterMissingRow(t *testing.T) {
	first := documentstore.Document{NormalizedURL: "one", ExtractedText: "needle first"}
	second := documentstore.Document{NormalizedURL: "two", ExtractedText: "needle evidence"}
	directory := &evidenceDocumentSetDirectory{
		documents: map[string]documentstore.Document{
			second.NormalizedURL: second,
		},
	}
	index, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "search.bleve"),
		directory,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	if err := index.Index(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if err := index.Index(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	beforeEvidence, err := index.Stats(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	enriched, err := index.SearchEvidence(
		t.Context(),
		SearchRequest{Query: "needle", Terms: []string{"needle"}},
		[]SearchResult{
			{DocumentID: "one", Snippet: "candidate"},
			{DocumentID: "two", Snippet: "candidate"},
		},
	)
	if err != nil || len(enriched) != 1 || enriched[0].DocumentID != "two" ||
		!strings.Contains(enriched[0].Snippet, "needle evidence") {
		t.Fatalf("enriched=%v error=%v", enriched, err)
	}
	afterEvidence, err := index.Stats(t.Context())
	if err != nil || afterEvidence.Documents != beforeEvidence.Documents ||
		!afterEvidence.UpdatedAt.Equal(beforeEvidence.UpdatedAt) {
		t.Fatalf(
			"evidence read changed stats from %#v to %#v: %v",
			beforeEvidence,
			afterEvidence,
			err,
		)
	}
}

func TestSearchEvidenceDocumentSetRejectsMalformedResults(t *testing.T) {
	directory := &evidenceDocumentSetDirectory{
		documents: map[string]documentstore.Document{
			"one": {NormalizedURL: "one", ExtractedText: "needle"},
		},
		malformed: true,
	}
	index := &BleveDiskIndex{documents: directory}
	_, err := index.SearchEvidence(
		t.Context(),
		SearchRequest{Query: "needle", Terms: []string{"needle"}},
		[]SearchResult{{DocumentID: "one"}},
	)
	if err == nil {
		t.Fatal("malformed document set accepted")
	}
}

func TestSearchEvidenceDocumentSetReturnsOperationalFailure(t *testing.T) {
	failure := errors.New("document set unavailable")
	directory := &evidenceDocumentSetDirectory{failure: failure}
	index := &BleveDiskIndex{documents: directory}
	_, err := index.SearchEvidence(
		t.Context(),
		SearchRequest{},
		[]SearchResult{{DocumentID: "one"}},
	)
	if !errors.Is(err, failure) {
		t.Fatalf("document set failure=%v, want=%v", err, failure)
	}
}

func TestSearchEvidenceDocumentSetRetainsStrictCandidatesAtDeadline(t *testing.T) {
	directory := &evidenceDocumentSetDirectory{failure: context.DeadlineExceeded}
	index := &BleveDiskIndex{documents: directory}
	results := []SearchResult{{DocumentID: "one"}, {DocumentID: "two"}}
	enriched, err := index.SearchEvidence(t.Context(), SearchRequest{}, results)
	if err != nil || len(enriched) != len(results) || enriched[1].DocumentID != "two" {
		t.Fatalf("enriched=%v error=%v", enriched, err)
	}
}

func TestSearchEvidenceDocumentSetDeadlineCannotAdmitRelaxedCandidates(t *testing.T) {
	directory := &evidenceDocumentSetDirectory{failure: context.Canceled}
	index := &BleveDiskIndex{documents: directory}
	results := []SearchResult{
		{DocumentID: "one", RelaxedRank: 1},
		{DocumentID: "two", RelaxedRank: 2},
	}
	enriched, err := index.SearchEvidence(
		t.Context(),
		SearchRequest{Relaxed: true},
		results,
	)
	if err != nil || len(enriched) != 0 {
		t.Fatalf("enriched=%v error=%v", enriched, err)
	}
}
