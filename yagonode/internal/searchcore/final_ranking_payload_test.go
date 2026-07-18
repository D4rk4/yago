package searchcore

import "testing"

func TestFinalRankingPayloadDropsConsumedEvidence(t *testing.T) {
	evidence := NewRankingEvidence(RankingSignalValue{Signal: SignalTitleScore, Value: 2.5})
	inner := stubSearcher{response: Response{Results: []Result{{
		URL:                         "https://example.test/",
		Evidence:                    evidence,
		EvidenceRequirementOrdinals: []int{0},
		FieldScores:                 map[string]float64{"title": 2.5},
		FieldTermPositions:          map[string]map[string][]int{"body": {"query": {3, 7}}},
		Explanation:                 "diagnostic tree",
	}}}}

	response, err := NewFinalRankingSearcher(inner).Search(t.Context(), Request{Limit: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	result := response.Results[0]
	if result.FieldTermPositions != nil || result.EvidenceRequirementOrdinals != nil ||
		result.FieldScores != nil || result.Explanation != "" {
		t.Fatalf("transient ranking payload retained: %+v", result)
	}
	if value, known := result.Evidence.Value(SignalTitleScore); !known || value != 2.5 {
		t.Fatalf("learned evidence = %v/%v", value, known)
	}
}

func TestFinalRankingPayloadPreservesExplicitExplanation(t *testing.T) {
	inner := stubSearcher{response: Response{Results: []Result{{
		URL:                "https://example.test/",
		FieldScores:        map[string]float64{"body": 1.5},
		FieldTermPositions: map[string]map[string][]int{"body": {"query": {2}}},
		Explanation:        "diagnostic tree",
	}}}}

	response, err := NewFinalRankingSearcher(inner).Search(
		t.Context(),
		Request{Limit: 1, Explain: true},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	result := response.Results[0]
	if result.FieldTermPositions != nil {
		t.Fatalf("consumed positions retained: %v", result.FieldTermPositions)
	}
	if result.FieldScores["body"] != 1.5 || result.Explanation != "diagnostic tree" {
		t.Fatalf("explicit explanation lost: %+v", result)
	}
}
