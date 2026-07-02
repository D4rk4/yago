package searchindex

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yacynode/internal/documentstore"
)

type fakeIndex struct{}

func (fakeIndex) Index(context.Context, documentstore.Document) error { return nil }

func (fakeIndex) Delete(context.Context, string) error { return nil }

func (fakeIndex) Search(context.Context, SearchRequest) (SearchResultSet, error) {
	return SearchResultSet{Results: []SearchResult{{URL: "https://example.org/"}}}, nil
}

func (fakeIndex) Stats(context.Context) (IndexStats, error) {
	return IndexStats{Documents: 1, Backend: "fake"}, nil
}

func TestSearchIndexContract(t *testing.T) {
	var index SearchIndex = fakeIndex{}

	results, err := index.Search(context.Background(), SearchRequest{Query: "example"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(results.Results))
	}
}
