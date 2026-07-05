package searchindex

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestFuzzySearchToleratesOneEditPerTerm(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{{
			NormalizedURL: "https://a.example/golang",
			Title:         "Golang tutorial",
			ExtractedText: "Learning golang from scratch.",
		}},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	exact, err := index.Search(t.Context(), SearchRequest{Query: "golnaX", MaxResults: 5})
	if err != nil {
		t.Fatalf("exact search: %v", err)
	}
	if exact.Total != 0 {
		t.Fatalf("misspelling matched without fuzzy: %#v", exact)
	}

	fuzzy, err := index.Search(t.Context(), SearchRequest{
		Query:      "golanX",
		MaxResults: 5,
		Fuzzy:      true,
	})
	if err != nil {
		t.Fatalf("fuzzy search: %v", err)
	}
	if fuzzy.Total != 1 || len(fuzzy.Results) != 1 {
		t.Fatalf("fuzzy search missed the close match: %#v", fuzzy)
	}
}
