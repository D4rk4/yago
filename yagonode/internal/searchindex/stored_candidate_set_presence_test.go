package searchindex

import (
	"context"
	"errors"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type batchPresenceDocumentDirectory struct {
	documents       map[string]documentstore.Document
	individualCalls int
	batchCalls      int
	documentCalls   int
	documentError   error
	batchError      error
	result          []bool
}

func (d *batchPresenceDocumentDirectory) Document(
	_ context.Context,
	normalizedURL string,
) (documentstore.Document, bool, error) {
	d.documentCalls++
	if d.documentError != nil {
		return documentstore.Document{}, false, d.documentError
	}
	document, found := d.documents[normalizedURL]

	return document, found, nil
}

func (d *batchPresenceDocumentDirectory) Count(context.Context) (int, error) {
	return len(d.documents), nil
}

func (d *batchPresenceDocumentDirectory) DocumentExists(
	context.Context,
	string,
) (bool, error) {
	d.individualCalls++

	return false, errors.New("individual presence checked")
}

func (d *batchPresenceDocumentDirectory) DocumentsExist(
	_ context.Context,
	normalizedURLs []string,
) ([]bool, error) {
	d.batchCalls++
	if d.batchError != nil {
		return nil, d.batchError
	}
	if d.result != nil {
		return append([]bool(nil), d.result...), nil
	}
	found := make([]bool, len(normalizedURLs))
	for index, normalizedURL := range normalizedURLs {
		_, found[index] = d.documents[normalizedURL]
	}

	return found, nil
}

func TestStoredCandidateSetPresenceUsesOneBatch(t *testing.T) {
	first := documentstore.Document{
		NormalizedURL: "https://example.org/first",
		Title:         "First",
	}
	missing := documentstore.Document{
		NormalizedURL: "https://example.org/missing",
		Title:         "Missing",
	}
	third := documentstore.Document{
		NormalizedURL: "https://example.org/third",
		Title:         "Third",
	}
	directory := &batchPresenceDocumentDirectory{
		documents: map[string]documentstore.Document{
			first.NormalizedURL: first,
			third.NormalizedURL: third,
		},
	}
	index := &BleveDiskIndex{
		documents:        directory,
		documentPresence: directory,
		storedCandidates: true,
	}
	hits := storedCandidateSetHits(t, first, missing, third)
	projections, found, err := index.loadSearchHitProjections(
		t.Context(),
		hits,
		SearchRequest{CandidateOnly: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if directory.batchCalls != 1 || directory.individualCalls != 0 {
		t.Fatalf(
			"presence calls batch=%d individual=%d",
			directory.batchCalls,
			directory.individualCalls,
		)
	}
	wantFound := []bool{true, false, true}
	for position := range wantFound {
		if found[position] != wantFound[position] {
			t.Fatalf("found[%d]=%t, want=%t", position, found[position], wantFound[position])
		}
	}
	if !projections[0].candidate || projections[0].document.Title != first.Title ||
		projections[1].candidate || !projections[2].candidate ||
		projections[2].document.Title != third.Title {
		t.Fatalf("projections=%#v", projections)
	}
}

func TestStoredCandidateSetPresenceRejectsBatchFailure(t *testing.T) {
	document := documentstore.Document{NormalizedURL: "https://example.org/one"}
	want := errors.New("batch presence failed")
	directory := &batchPresenceDocumentDirectory{
		documents:  map[string]documentstore.Document{document.NormalizedURL: document},
		batchError: want,
	}
	index := &BleveDiskIndex{
		documents:        directory,
		documentPresence: directory,
		storedCandidates: true,
	}
	if _, _, err := index.loadSearchHitProjections(
		t.Context(),
		storedCandidateSetHits(t, document),
		SearchRequest{CandidateOnly: true},
	); !errors.Is(err, want) {
		t.Fatalf("batch presence error=%v, want=%v", err, want)
	}
	if directory.individualCalls != 0 {
		t.Fatalf("individual presence calls=%d", directory.individualCalls)
	}
}

func TestStoredCandidateSetPresenceRejectsMisalignedResult(t *testing.T) {
	document := documentstore.Document{NormalizedURL: "https://example.org/one"}
	directory := &batchPresenceDocumentDirectory{
		documents: map[string]documentstore.Document{document.NormalizedURL: document},
		result:    []bool{},
	}
	index := &BleveDiskIndex{
		documents:        directory,
		documentPresence: directory,
		storedCandidates: true,
	}
	if _, _, err := index.loadSearchHitProjections(
		t.Context(),
		storedCandidateSetHits(t, document),
		SearchRequest{CandidateOnly: true},
	); err == nil {
		t.Fatal("misaligned batch presence result accepted")
	}
}

func TestStoredCandidateSetPresenceFallsBackForMalformedProjection(t *testing.T) {
	first := documentstore.Document{NormalizedURL: "https://example.org/first", Title: "First"}
	second := documentstore.Document{NormalizedURL: "https://example.org/second", Title: "Second"}
	directory := &batchPresenceDocumentDirectory{documents: map[string]documentstore.Document{
		first.NormalizedURL:  first,
		second.NormalizedURL: second,
	}}
	index := &BleveDiskIndex{
		documents:        directory,
		documentPresence: directory,
		storedCandidates: true,
	}
	hits := storedCandidateSetHits(t, first, second)
	hits[1].Fields[storedCandidateField] = "{"
	projections, found, err := index.loadSearchHitProjections(
		t.Context(),
		hits,
		SearchRequest{CandidateOnly: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if directory.batchCalls != 1 || directory.documentCalls != 1 ||
		!found[0] || !found[1] || !projections[0].candidate || projections[1].candidate {
		t.Fatalf(
			"calls=%d/%d found=%v candidates=%t/%t",
			directory.batchCalls,
			directory.documentCalls,
			found,
			projections[0].candidate,
			projections[1].candidate,
		)
	}
}

func TestStoredCandidateSetPresenceRejectsFallbackFailure(t *testing.T) {
	document := documentstore.Document{NormalizedURL: "https://example.org/malformed"}
	want := errors.New("document read failed")
	directory := &batchPresenceDocumentDirectory{
		documents:     map[string]documentstore.Document{document.NormalizedURL: document},
		documentError: want,
	}
	index := &BleveDiskIndex{
		documents:        directory,
		documentPresence: directory,
		storedCandidates: true,
	}
	hit := storedCandidateSetHits(t, document)[0]
	hit.Fields[storedCandidateField] = "{"
	if _, _, err := index.loadSearchHitProjections(
		t.Context(),
		[]*search.DocumentMatch{hit},
		SearchRequest{CandidateOnly: true},
	); !errors.Is(err, want) {
		t.Fatalf("fallback error=%v, want=%v", err, want)
	}
}

func TestCollectHitsUsesPreparedStoredCandidatePresence(t *testing.T) {
	document := documentstore.Document{
		NormalizedURL: "https://example.org/prepared",
		Title:         "Prepared",
	}
	directory := &batchPresenceDocumentDirectory{documents: map[string]documentstore.Document{
		document.NormalizedURL: document,
	}}
	index := &BleveDiskIndex{
		documents:        directory,
		documentPresence: directory,
		storedCandidates: true,
	}
	hit := storedCandidateSetHits(t, document)[0]
	hit.Score = 1
	result, orphans, err := index.collectHits(
		t.Context(),
		SearchRequest{MaxResults: 1, CandidateOnly: true},
		&bleve.SearchResult{Total: 1, Hits: search.DocumentMatchCollection{hit}},
	)
	if err != nil || result.Total != 1 || len(result.Results) != 1 || len(orphans) != 0 ||
		directory.batchCalls != 1 || directory.documentCalls != 0 {
		t.Fatalf(
			"result=%#v orphans=%v calls=%d/%d error=%v",
			result,
			orphans,
			directory.batchCalls,
			directory.documentCalls,
			err,
		)
	}
}

func TestCollectHitsRejectsStoredCandidatePresenceFailure(t *testing.T) {
	document := documentstore.Document{NormalizedURL: "https://example.org/failure"}
	want := errors.New("batch presence failed")
	directory := &batchPresenceDocumentDirectory{
		documents:  map[string]documentstore.Document{document.NormalizedURL: document},
		batchError: want,
	}
	index := &BleveDiskIndex{
		documents:        directory,
		documentPresence: directory,
		storedCandidates: true,
	}
	hit := storedCandidateSetHits(t, document)[0]
	if _, _, err := index.collectHits(
		t.Context(),
		SearchRequest{MaxResults: 1, CandidateOnly: true},
		&bleve.SearchResult{Total: 1, Hits: search.DocumentMatchCollection{hit}},
	); !errors.Is(err, want) {
		t.Fatalf("collect error=%v, want=%v", err, want)
	}
}

func storedCandidateSetHits(
	t *testing.T,
	documents ...documentstore.Document,
) []*search.DocumentMatch {
	t.Helper()
	hits := make([]*search.DocumentMatch, len(documents))
	for index, document := range documents {
		encoded, err := encodeStoredCandidateProjection(document)
		if err != nil {
			t.Fatal(err)
		}
		hits[index] = &search.DocumentMatch{
			ID: document.NormalizedURL,
			Fields: map[string]any{
				storedCandidateField: encoded,
			},
		}
	}

	return hits
}
