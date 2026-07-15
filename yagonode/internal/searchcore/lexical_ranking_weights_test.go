package searchcore

import (
	"math"
	"testing"
)

func TestLexicalBlendControlsRetrievalAndLexicalOrder(t *testing.T) {
	results := []Result{
		{URL: "retrieval", Score: 1, Title: "unrelated"},
		{URL: "lexical", Score: 0.9, Title: "alpha beta"},
		{URL: "tail", Score: 0.8, Title: "alpha"},
	}
	request := Request{Terms: []string{"alpha", "beta"}}
	retrieval := rerankLexicalProximityWithWeights(
		results,
		request,
		LexicalRankingWeights{Blend: 0},
	)
	if retrieval[0].URL != "retrieval" {
		t.Fatalf("zero blend order = %v", urls(retrieval))
	}
	lexical := rerankLexicalProximityWithWeights(
		results,
		request,
		LexicalRankingWeights{Blend: 1},
	)
	if lexical[0].URL != "lexical" {
		t.Fatalf("full blend order = %v", urls(lexical))
	}
}

func TestLexicalGapAgreementWeightChangesRankingEvidence(t *testing.T) {
	results := []Result{
		positionedResult("original-gap", 1, 3),
		positionedResult("near-forward", 1, 4),
		positionedResult("reverse", 4, 1),
	}
	request := Request{Terms: []string{"alpha", "and", "beta"}}
	without := rerankLexicalProximityWithWeights(
		results,
		request,
		LexicalRankingWeights{Blend: 1, GapAgreement: 0},
	)
	with := rerankLexicalProximityWithWeights(
		results,
		request,
		LexicalRankingWeights{Blend: 1, GapAgreement: 1},
	)
	if resultDiversityRelevance(resultByURL(with, "original-gap")) <=
		resultDiversityRelevance(resultByURL(without, "original-gap")) {
		t.Fatal("gap agreement weight did not increase exact-gap evidence")
	}
}

func TestLexicalEvidenceSearcherReadsWeightsForEveryRequest(t *testing.T) {
	reads := 0
	weights := LexicalRankingWeights{Blend: 0}
	inner := stubSearcher{response: Response{Results: []Result{
		{URL: "retrieval", Score: 1, Title: "unrelated"},
		{URL: "lexical", Score: 0.9, Title: "alpha beta"},
		{URL: "tail", Score: 0.8, Title: "alpha"},
	}}}
	searcher := NewLexicalEvidenceSearcherWithWeights(inner, func() LexicalRankingWeights {
		reads++

		return weights
	})
	request := Request{Terms: []string{"alpha", "beta"}}
	first, err := searcher.Search(t.Context(), request)
	if err != nil {
		t.Fatalf("first search: %v", err)
	}
	weights.Blend = 1
	second, err := searcher.Search(t.Context(), request)
	if err != nil {
		t.Fatalf("second search: %v", err)
	}
	if reads != 2 || first.Results[0].URL != "retrieval" ||
		second.Results[0].URL != "lexical" {
		t.Fatalf(
			"reads = %d, first = %v, second = %v",
			reads,
			urls(first.Results),
			urls(second.Results),
		)
	}
}

func TestLexicalRankingWeightsBoundsAndInvalidProvider(t *testing.T) {
	bounded := lexicalRankingWeights(func() LexicalRankingWeights {
		return LexicalRankingWeights{Blend: -1, GapAgreement: 2}
	})
	if bounded != (LexicalRankingWeights{Blend: 0, GapAgreement: 1}) {
		t.Fatalf("bounded weights = %+v", bounded)
	}
	invalid := lexicalRankingWeights(func() LexicalRankingWeights {
		return LexicalRankingWeights{Blend: math.NaN()}
	})
	if invalid != DefaultLexicalRankingWeights() {
		t.Fatalf("invalid weights = %+v", invalid)
	}
}

func TestLexicalRerankSearcherWithWeights(t *testing.T) {
	searcher := NewLexicalRerankSearcherWithWeights(
		stubSearcher{response: Response{Results: []Result{
			{URL: "retrieval", Score: 1, Title: "unrelated"},
			{URL: "lexical", Score: 0.9, Title: "alpha beta"},
			{URL: "tail", Score: 0.8, Title: "alpha"},
		}}},
		func() LexicalRankingWeights { return LexicalRankingWeights{Blend: 1} },
	)
	response, err := searcher.Search(t.Context(), Request{Terms: []string{"alpha", "beta"}})
	if err != nil || response.Results[0].URL != "lexical" {
		t.Fatalf("response = %+v, err = %v", response, err)
	}
}

func resultByURL(results []Result, target string) Result {
	for _, result := range results {
		if result.URL == target {
			return result
		}
	}

	return Result{}
}
