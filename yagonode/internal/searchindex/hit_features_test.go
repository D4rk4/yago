package searchindex

import (
	"slices"
	"testing"

	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestHitFieldTermPositionsOmittedUnlessRequested(t *testing.T) {
	hit := &search.DocumentMatch{Locations: search.FieldTermLocationMap{
		"body": {"linux": {{Pos: 2}}},
	}}
	if got := hitFieldTermPositions(SearchRequest{IncludePositions: false}, hit); got != nil {
		t.Fatalf("positions returned without request: %v", got)
	}
}

func TestHitFieldTermPositionsNilWithoutLocations(t *testing.T) {
	hit := &search.DocumentMatch{}
	if got := hitFieldTermPositions(SearchRequest{IncludePositions: true}, hit); got != nil {
		t.Fatalf("positions returned without locations: %v", got)
	}
}

func TestHitFieldTermPositionsSortsPositions(t *testing.T) {
	hit := &search.DocumentMatch{Locations: search.FieldTermLocationMap{
		"body": {"kernel": {{Pos: 8}, {Pos: 3}}, "linux": {{Pos: 2}}},
	}}
	got := hitFieldTermPositions(SearchRequest{IncludePositions: true}, hit)
	if want := []int{3, 8}; !slices.Equal(got["body"]["kernel"], want) {
		t.Fatalf("kernel positions = %v, want %v", got["body"]["kernel"], want)
	}
	if want := []int{2}; !slices.Equal(got["body"]["linux"], want) {
		t.Fatalf("linux positions = %v, want %v", got["body"]["linux"], want)
	}
}

func TestHitFieldScoresOmittedUnlessExplained(t *testing.T) {
	hit := &search.DocumentMatch{Expl: &search.Explanation{Value: 1}}
	if got := hitFieldScores(SearchRequest{Explain: false}, hit); got != nil {
		t.Fatalf("scores returned without explain: %v", got)
	}
}

func TestHitFieldScoresNilWithoutExplanation(t *testing.T) {
	hit := &search.DocumentMatch{}
	if got := hitFieldScores(SearchRequest{Explain: true}, hit); got != nil {
		t.Fatalf("scores returned without an explanation tree: %v", got)
	}
}

func TestHitFieldScoresNilWhenNoWeightNodes(t *testing.T) {
	hit := &search.DocumentMatch{Expl: &search.Explanation{
		Message:  "sum of:",
		Children: []*search.Explanation{{Message: "coord(1/1)"}},
	}}
	if got := hitFieldScores(SearchRequest{Explain: true}, hit); got != nil {
		t.Fatalf("scores returned for a tree with no weight nodes: %v", got)
	}
}

func TestHitFieldScoresDeduplicatesPerFieldTerm(t *testing.T) {
	hit := &search.DocumentMatch{Expl: &search.Explanation{
		Message: "sum of:",
		Children: []*search.Explanation{
			{Value: 0.0118, Message: "weight(title:linux^6.000000 in doc), product of:"},
			// The same field:term repeats across query clauses and must collapse.
			{Value: 0.0118, Message: "weight(title:linux^6.000000 in doc), product of:"},
			{Value: 0.0200, Message: "weight(title:kernel^6.000000 in doc), product of:"},
			{Value: 0.0039, Message: "weight(url:linux^2.000000 in doc), product of:"},
		},
	}}
	got := hitFieldScores(SearchRequest{Explain: true}, hit)
	if diff := got["title"] - 0.0318; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("title score = %v, want 0.0318 (0.0118 deduped + 0.0200)", got["title"])
	}
	if diff := got["url"] - 0.0039; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("url score = %v, want 0.0039", got["url"])
	}
}

func TestCollectWeightNodesToleratesNil(t *testing.T) {
	// The recursion guards against a nil child; calling it directly must not panic.
	collectWeightNodes(nil, map[string]map[string]float64{})
}

func TestSearchSurfacesFieldFeaturesEndToEnd(t *testing.T) {
	stored := &fakeStoredDocuments{documents: []documentstore.Document{{
		NormalizedURL: "https://a.example/linux",
		Title:         "linux kernel guide",
		ExtractedText: "the linux kernel scheduler manages processes; the kernel is core",
	}}}
	index, err := NewBleveMemoryIndex(t.Context(), stored)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	set, err := index.Search(t.Context(), SearchRequest{
		Query:            "linux kernel",
		Terms:            []string{"linux", "kernel"},
		MaxResults:       5,
		Explain:          true,
		IncludePositions: true,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(set.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(set.Results))
	}
	result := set.Results[0]
	// The document is under the scored-word floor, so the quality prior is neutral
	// rather than left at the zero value — proving it was computed and mapped.
	if result.Quality != 1.0 {
		t.Errorf("Quality = %v, want the neutral 1.0 for short text", result.Quality)
	}
	// "linux" and "kernel" sit adjacent in the body, so the single query-word pair
	// co-occurs within the window and the SDM proximity feature is fully satisfied.
	if result.Proximity != 1.0 {
		t.Errorf("Proximity = %v, want 1.0 for the adjacent query terms", result.Proximity)
	}
	if result.FieldScores["title"] <= result.FieldScores["body"] {
		t.Errorf("title score %v must exceed body score %v (title is boosted)",
			result.FieldScores["title"], result.FieldScores["body"])
	}
	if want := []int{3, 8}; !slices.Equal(result.FieldTermPositions["body"]["kernel"], want) {
		t.Errorf("body kernel positions = %v, want %v",
			result.FieldTermPositions["body"]["kernel"], want)
	}
}
