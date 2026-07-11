package searchindex

import (
	"testing"

	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestSearchResultCarriesStoredContentClusterAssignment(t *testing.T) {
	result := searchResultFromDocument(
		&search.DocumentMatch{ID: "document"},
		documentstore.Document{
			NormalizedURL:     "https://a.example",
			ClusterID:         "cluster-a",
			RepresentativeURL: "https://canonical.example",
		},
		SearchRequest{},
	)
	if result.ClusterID != "cluster-a" ||
		result.RepresentativeURL != "https://canonical.example" {
		t.Fatalf("search cluster assignment = %+v", result)
	}
}
