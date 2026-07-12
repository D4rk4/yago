package searchindex

import (
	"context"
	"errors"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type changingDocumentDirectory struct {
	document    documentstore.Document
	lookups     int
	reloadErr   error
	reloadFound bool
}

func (d *changingDocumentDirectory) Document(
	context.Context,
	string,
) (documentstore.Document, bool, error) {
	d.lookups++
	if d.lookups == 1 {
		return d.document, true, nil
	}
	if d.reloadErr != nil {
		return documentstore.Document{}, false, d.reloadErr
	}

	return d.document, d.reloadFound, nil
}

func (d *changingDocumentDirectory) Count(context.Context) (int, error) {
	return 1, nil
}

func completeSearchFixtureResult(documentID string) *bleve.SearchResult {
	return &bleve.SearchResult{
		Status: &bleve.SearchStatus{Total: 1, Successful: 1},
		Total:  1,
		Hits: search.DocumentMatchCollection{&search.DocumentMatch{
			ID:          documentID,
			Score:       1,
			DecodedSort: []string{documentID},
		}},
	}
}

func TestCompleteSearchSurfacesFinalDocumentReloadFailure(t *testing.T) {
	sentinel := errors.New("reload failed")
	document := documentstore.Document{
		NormalizedURL: "https://example.org/needle",
		ExtractedText: "needle text",
	}
	directory := &changingDocumentDirectory{document: document, reloadErr: sentinel}
	index := &BleveDiskIndex{
		alias: searchErrorBleveIndex{
			result: completeSearchFixtureResult(document.NormalizedURL),
		},
		documents: directory,
	}

	_, _, err := index.searchCompleteHitsWithin(
		t.Context(),
		SearchRequest{Query: "needle", MaxResults: 1, WithFacets: true},
		1,
		1,
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("reload error = %v, want %v", err, sentinel)
	}
}

func TestCompleteSearchDropsDocumentRemovedAfterCollection(t *testing.T) {
	document := documentstore.Document{
		NormalizedURL: "https://example.org/needle",
		ExtractedText: "needle text",
	}
	directory := &changingDocumentDirectory{document: document}
	index := &BleveDiskIndex{
		alias: searchErrorBleveIndex{
			result: completeSearchFixtureResult(document.NormalizedURL),
		},
		documents: directory,
	}

	result, _, err := index.searchCompleteHitsWithin(
		t.Context(),
		SearchRequest{Query: "needle", MaxResults: 1, WithFacets: true},
		1,
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || len(result.Results) != 0 {
		t.Fatalf("result after removal = %#v", result)
	}
}

func TestFinalCompleteResultsSurfaceEvidenceCancellation(t *testing.T) {
	document := documentstore.Document{
		NormalizedURL: "https://example.org/needle",
		ExtractedText: "needle text",
	}
	index := &BleveDiskIndex{documents: newFakeDocumentDirectory(document)}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := index.finalCompleteResults(
		ctx,
		SearchRequest{Query: "needle", MaxResults: 1},
		[]*search.DocumentMatch{{ID: document.NormalizedURL}},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("evidence error = %v, want %v", err, context.Canceled)
	}
}

func TestCollectHitsAppliesStoredDocumentFilters(t *testing.T) {
	document := documentstore.Document{
		NormalizedURL: "https://example.org/needle",
		ExtractedText: "needle text",
		Language:      "en",
	}
	index := &BleveDiskIndex{documents: newFakeDocumentDirectory(document)}

	result, _, err := index.collectHits(
		t.Context(),
		SearchRequest{Query: "needle", MaxResults: 1, Language: "de"},
		completeSearchFixtureResult(document.NormalizedURL),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 0 || len(result.Results) != 0 {
		t.Fatalf("filtered result = %#v", result)
	}
}

func TestCollectHitsSurfacesEvidenceCancellation(t *testing.T) {
	document := documentstore.Document{
		NormalizedURL: "https://example.org/needle",
		ExtractedText: "needle text",
	}
	index := &BleveDiskIndex{documents: newFakeDocumentDirectory(document)}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := index.collectHits(
		ctx,
		SearchRequest{Query: "needle", MaxResults: 1},
		completeSearchFixtureResult(document.NormalizedURL),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("evidence error = %v, want %v", err, context.Canceled)
	}
}

func TestMemorySearchSurfacesEvidenceCancellation(t *testing.T) {
	document := documentstore.Document{
		NormalizedURL: "https://example.org/needle",
		ExtractedText: "needle text",
	}
	index := &BleveMemoryIndex{
		index: searchErrorBleveIndex{result: completeSearchFixtureResult(
			document.NormalizedURL,
		)},
		documents: map[string]documentstore.Document{
			document.NormalizedURL: document,
		},
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := index.Search(ctx, SearchRequest{Query: "needle", MaxResults: 1})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("evidence error = %v, want %v", err, context.Canceled)
	}
}
