package searchindex

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func writeLegacySearchIndex(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "search.bleve")
	legacy, err := bleve.New(path, bleve.NewIndexMapping())
	if err != nil {
		t.Fatalf("create legacy index: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy index: %v", err)
	}

	return path
}

func TestNewBleveDiskIndexMigratesLegacyMapping(t *testing.T) {
	path := writeLegacySearchIndex(t)
	doc := documentstore.Document{
		NormalizedURL:  "https://example.org/news",
		Title:          "Зеленский",
		ExtractedText:  "Новости про Зеленского.",
		Language:       "ru",
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

	if !index.analyzerScope || stored.scans != 1 {
		t.Fatalf(
			"legacy migration scope=%v scans=%d",
			index.analyzerScope,
			stored.scans,
		)
	}
	results, err := index.Search(
		t.Context(),
		SearchRequest{Query: "зеленски", MaxResults: 5, Fuzzy: true},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if results.Total != 1 || len(results.Results) != 1 {
		t.Fatalf("results = %#v", results)
	}
}

func TestBleveDiskIndexServesLegacyMappingWithoutRebuildSource(t *testing.T) {
	path := writeLegacySearchIndex(t)

	index, err := NewBleveDiskIndex(t.Context(), path, newFakeDocumentDirectory(), nil)
	if err != nil {
		t.Fatalf("NewBleveDiskIndex: %v", err)
	}
	t.Cleanup(func() { _ = index.Close() })

	if index.analyzerScope || index.storedCandidates {
		t.Fatal("legacy index without a rebuild source claimed current mapping")
	}
	results, err := index.Search(t.Context(), SearchRequest{Query: "golang", MaxResults: 5})
	if err != nil {
		t.Fatalf("Search on legacy index: %v", err)
	}
	if results.Total != 0 {
		t.Fatalf("results = %#v", results)
	}
}
