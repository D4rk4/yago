package searchlocal

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestSearchCandidatesSkipsRelaxationWhenStrictWindowIsFull(t *testing.T) {
	index := &candidateIndex{
		strict: searchindex.SearchResultSet{Results: []searchindex.SearchResult{
			{DocumentID: "strict-one"},
			{DocumentID: "strict-two"},
		}, Total: 3},
		relaxed: searchindex.SearchResultSet{Results: []searchindex.SearchResult{{
			DocumentID: "relaxed",
		}}},
	}
	set, err := (localSearcher{index: index}).searchCandidates(
		t.Context(),
		searchindex.SearchRequest{
			Query:      "adult giraffe weight",
			Terms:      []string{"adult", "giraffe", "weight"},
			MaxResults: 2,
		},
	)
	if err != nil {
		t.Fatalf("searchCandidates: %v", err)
	}
	if len(index.requests) != 1 || len(set.Results) != 2 {
		t.Fatalf("requests=%#v results=%#v", index.requests, set.Results)
	}
}

func TestSearchCandidatesRetainsRelaxationAtStrictPaginationBoundary(t *testing.T) {
	index := &candidateIndex{
		strict: searchindex.SearchResultSet{Results: []searchindex.SearchResult{
			{DocumentID: "strict-one"},
			{DocumentID: "strict-two"},
		}, Total: 2},
		relaxed: searchindex.SearchResultSet{
			Results: []searchindex.SearchResult{
				{DocumentID: "strict-one"},
				{DocumentID: "strict-two"},
				{DocumentID: "relaxed"},
			},
			Total: 3,
		},
	}
	set, err := (localSearcher{index: index}).searchCandidates(
		t.Context(),
		searchindex.SearchRequest{
			Query:      "adult giraffe weight",
			Terms:      []string{"adult", "giraffe", "weight"},
			MaxResults: 2,
		},
	)
	if err != nil {
		t.Fatalf("searchCandidates: %v", err)
	}
	if len(index.requests) != 2 || set.Total != 3 {
		t.Fatalf("requests=%#v total=%d", index.requests, set.Total)
	}
}

func TestSearchCandidatesRetainsRelaxedFacetPassForFullStrictWindow(t *testing.T) {
	index := &candidateIndex{
		strict: searchindex.SearchResultSet{Results: []searchindex.SearchResult{
			{DocumentID: "strict"},
		}},
		relaxed: searchindex.SearchResultSet{
			Results: []searchindex.SearchResult{{DocumentID: "relaxed"}},
			Facets:  []searchindex.FacetGroup{{Name: "host"}},
		},
	}
	set, err := (localSearcher{index: index}).searchCandidates(
		t.Context(),
		searchindex.SearchRequest{
			Query:      "adult giraffe weight",
			Terms:      []string{"adult", "giraffe", "weight"},
			MaxResults: 1,
			WithFacets: true,
		},
	)
	if err != nil {
		t.Fatalf("searchCandidates: %v", err)
	}
	if len(index.requests) != 2 || len(set.Facets) != 1 {
		t.Fatalf("requests=%#v facets=%#v", index.requests, set.Facets)
	}
}
