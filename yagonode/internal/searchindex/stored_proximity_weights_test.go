package searchindex

import "testing"

func TestStoredProximityWeightsAreIndependent(t *testing.T) {
	base := []SearchResult{
		{DocumentID: "ordered", Score: 1, OrderedProximity: 1},
		{DocumentID: "unordered", Score: 1, Proximity: 1},
	}
	request := SearchRequest{
		Terms:            []string{"one", "two"},
		IncludePositions: true,
		Weights:          DefaultRankingWeights(),
	}
	request.Weights.OrderedProximity = 1
	request.Weights.Proximity = 0
	ordered := append([]SearchResult(nil), base...)
	rescoreStoredProximity(ordered, request)
	if ordered[0].DocumentID != "ordered" || ordered[0].Score != 2 ||
		ordered[1].Score != 1 {
		t.Fatalf("ordered weights = %#v", ordered)
	}

	request.Weights.OrderedProximity = 0
	request.Weights.Proximity = 1
	unordered := append([]SearchResult(nil), base...)
	rescoreStoredProximity(unordered, request)
	if unordered[0].DocumentID != "unordered" || unordered[0].Score != 2 ||
		unordered[1].Score != 1 {
		t.Fatalf("unordered weights = %#v", unordered)
	}

	request.Weights.Proximity = 0
	disabled := append([]SearchResult(nil), base...)
	rescoreStoredProximity(disabled, request)
	if disabled[0].Score != 1 || disabled[1].Score != 1 {
		t.Fatalf("disabled weights changed scores: %#v", disabled)
	}
}
