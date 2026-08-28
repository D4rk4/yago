package searchindex

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type searchReadCompletion struct {
	result SearchResultSet
	err    error
}

func TestBleveDiskSearchDoesNotWaitForMutationWhenCandidateIsMissing(t *testing.T) {
	doc := documentstore.Document{
		NormalizedURL: "https://example.org/missing",
		ExtractedText: "needle",
	}
	directory := newFakeDocumentDirectory(doc)
	index, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "search.bleve"),
		directory,
		&fakeStoredDocuments{documents: []documentstore.Document{doc}},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	delete(directory.documents, doc.NormalizedURL)
	before, err := index.Stats(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	index.mutationMu.Lock()
	finished := make(chan searchReadCompletion, 1)
	go func() {
		result, searchErr := index.Search(t.Context(), SearchRequest{
			Query: "needle", MaxResults: 1, CandidateOnly: true,
		})
		finished <- searchReadCompletion{result: result, err: searchErr}
	}()

	var completion searchReadCompletion
	select {
	case completion = <-finished:
		index.mutationMu.Unlock()
	case <-time.After(time.Second):
		index.mutationMu.Unlock()
		completion = <-finished
		t.Fatalf("search waited for index mutation: %v", completion.err)
	}
	if completion.err != nil || completion.result.Total != 0 ||
		len(completion.result.Results) != 0 {
		t.Fatalf("result=%#v error=%v", completion.result, completion.err)
	}
	after, err := index.Stats(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if after.Documents != before.Documents || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("stats changed from %#v to %#v", before, after)
	}
}

func TestBleveDiskSearchLeavesPresentCandidateIndexUnchanged(t *testing.T) {
	doc := documentstore.Document{
		NormalizedURL: "https://example.org/present",
		ExtractedText: "needle",
	}
	directory := newFakeDocumentDirectory(doc)
	index, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "search.bleve"),
		directory,
		&fakeStoredDocuments{documents: []documentstore.Document{doc}},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	before, err := index.Stats(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	result, err := index.Search(t.Context(), SearchRequest{
		Query: "needle", MaxResults: 1, CandidateOnly: true,
	})
	if err != nil || result.Total != 1 || len(result.Results) != 1 {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	after, err := index.Stats(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if after.Documents != before.Documents || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("stats changed from %#v to %#v", before, after)
	}
}

func TestBleveDiskSearchEvidenceLeavesMissingDocumentIndexUnchanged(t *testing.T) {
	doc := documentstore.Document{
		NormalizedURL: "https://example.org/missing-evidence",
		ExtractedText: "needle evidence",
	}
	directory := newFakeDocumentDirectory(doc)
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
	if err := index.Index(t.Context(), doc); err != nil {
		t.Fatal(err)
	}
	before, err := index.Stats(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	delete(directory.documents, doc.NormalizedURL)

	evidence, err := index.SearchEvidence(
		t.Context(),
		SearchRequest{Query: "needle"},
		[]SearchResult{{DocumentID: doc.NormalizedURL}},
	)
	if err != nil || len(evidence) != 0 {
		t.Fatalf("evidence=%#v error=%v", evidence, err)
	}
	after, err := index.Stats(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if after.Documents != before.Documents || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("stats changed from %#v to %#v", before, after)
	}
}
