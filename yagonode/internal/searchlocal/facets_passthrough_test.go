package searchlocal

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestSearcherPassesFacetsThrough(t *testing.T) {
	index := &fakeIndex{response: searchindex.SearchResultSet{
		Total: 1,
		Results: []searchindex.SearchResult{
			{Title: "Go", URL: "https://a.example/x", Score: 1},
		},
		Facets: []searchindex.FacetGroup{{
			Name:  "host",
			Terms: []searchindex.FacetTerm{{Term: "a.example", Count: 1}},
		}},
	}}

	resp, err := NewSearcher(index).Search(t.Context(), searchcore.Request{
		Query: "go", WithFacets: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !index.got.WithFacets {
		t.Fatal("WithFacets not forwarded to the index")
	}
	if len(resp.Facets) != 1 || resp.Facets[0].Name != "host" ||
		resp.Facets[0].Terms[0].Term != "a.example" {
		t.Fatalf("facets = %+v", resp.Facets)
	}
}
