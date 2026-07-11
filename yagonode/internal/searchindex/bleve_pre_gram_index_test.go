package searchindex

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

// writePreGramIndex creates an on-disk index the way releases before the
// trigram analyzer did: a mapping with no text_gram analyzer registered.
func writePreGramIndex(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "search.bleve")
	old, err := bleve.New(path, bleve.NewIndexMapping())
	if err != nil {
		t.Fatalf("create pre-gram index: %v", err)
	}
	if err := old.Close(); err != nil {
		t.Fatalf("close pre-gram index: %v", err)
	}

	return path
}

func TestNewBleveDiskIndexMigratesPreGramIndex(t *testing.T) {
	path := writePreGramIndex(t)
	doc := documentstore.Document{
		NormalizedURL:  "https://example.org/news",
		Title:          "Зеленский",
		ExtractedText:  "Новости про Зеленского.",
		FetchedAt:      time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
		PublishedAt:    time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
		DateConfidence: 1,
	}
	stored := &fakeStoredDocuments{documents: []documentstore.Document{doc}}

	index, err := NewBleveDiskIndex(t.Context(), path, newFakeDocumentDirectory(doc), stored)
	if err != nil {
		t.Fatalf("NewBleveDiskIndex: %v", err)
	}
	t.Cleanup(func() { _ = index.Close() })

	if !index.gram || stored.scans != 1 {
		t.Fatalf(
			"pre-gram index should be rebuilt under the current mapping: gram=%v scans=%d",
			index.gram,
			stored.scans,
		)
	}
	// The morphological variant only matches through the gram fields (recovery
	// path), proving the migrated index carries the current mapping.
	results, err := index.Search(
		t.Context(),
		SearchRequest{Query: "зеленски", MaxResults: 5, Fuzzy: true},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if results.Total != 1 || len(results.Results) != 1 {
		t.Fatalf("results = %#v, want the morphological match", results)
	}
}

func TestBleveDiskIndexServesPreGramIndexWithoutRebuildSource(t *testing.T) {
	path := writePreGramIndex(t)

	index, err := NewBleveDiskIndex(t.Context(), path, newFakeDocumentDirectory(), nil)
	if err != nil {
		t.Fatalf("NewBleveDiskIndex: %v", err)
	}
	t.Cleanup(func() { _ = index.Close() })

	if index.gram {
		t.Fatal("a pre-gram index kept without a rebuild source must not claim gram support")
	}
	// Before the fix this failed the whole search with
	// "no analyzer named 'text_gram' registered".
	results, err := index.Search(t.Context(), SearchRequest{Query: "golang", MaxResults: 5})
	if err != nil {
		t.Fatalf("Search on a pre-gram index: %v", err)
	}
	if results.Total != 0 {
		t.Fatalf("results = %#v", results)
	}
}
