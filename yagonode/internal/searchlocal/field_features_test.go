package searchlocal

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestSearcherRequestsPositionsByTermCount(t *testing.T) {
	cases := []struct {
		name string
		req  searchcore.Request
		want bool
	}{
		{"two parsed terms", searchcore.Request{Terms: []string{"a", "b"}}, true},
		{"two query words", searchcore.Request{Query: "linux kernel"}, true},
		{"single word", searchcore.Request{Query: "linux"}, false},
	}
	for _, tc := range cases {
		index := &fakeIndex{}
		if _, err := NewSearcher(index).Search(t.Context(), tc.req); err != nil {
			t.Fatalf("%s: search: %v", tc.name, err)
		}
		if index.got.IncludePositions != tc.want {
			t.Errorf(
				"%s: IncludePositions = %v, want %v",
				tc.name,
				index.got.IncludePositions,
				tc.want,
			)
		}
	}
}

func TestSearcherMapsFieldFeatures(t *testing.T) {
	index := &fakeIndex{response: searchindex.SearchResultSet{
		Total: 1,
		Results: []searchindex.SearchResult{{
			Title:              "linux",
			URL:                "https://a.example/",
			Score:              1,
			FieldScores:        map[string]float64{"title": 2.5},
			FieldTermPositions: map[string]map[string][]int{"body": {"kernel": {1, 2}}},
		}},
	}}
	resp, err := NewSearcher(index).Search(t.Context(), searchcore.Request{Query: "linux kernel"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(resp.Results))
	}
	if resp.Results[0].FieldScores["title"] != 2.5 {
		t.Errorf("FieldScores not mapped: %v", resp.Results[0].FieldScores)
	}
	positions := resp.Results[0].FieldTermPositions["body"]["kernel"]
	if len(positions) != 2 || positions[0] != 1 || positions[1] != 2 {
		t.Errorf("FieldTermPositions not mapped: %v", resp.Results[0].FieldTermPositions)
	}
}
