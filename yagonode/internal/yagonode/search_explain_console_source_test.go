package yagonode

import (
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/learnedrank"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestSearchExplainEndpointBacksAdminConsole(t *testing.T) {
	index := &searchExplainScript{result: searchindex.SearchResultSet{
		Results: []searchindex.SearchResult{
			{
				URL:          "https://example.test/alpha",
				Score:        4,
				Quality:      0.8,
				QualityKnown: true,
				StrictRank:   1,
				StrictScore:  4,
				FieldScores:  map[string]float64{"title": 3, "body": 1},
				Explanation:  "retrieval evidence",
			},
			{URL: "https://example.test/beta", Score: 1},
		},
	}}
	explanation, err := newSearchExplainEndpoint(
		index, nil, nil, activeExplainRanker(t), nil,
	).Explain(t.Context(), " alpha ", false)
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if explanation.Query != "alpha" || explanation.ModelRevision != "explain-v1" ||
		explanation.ModelKind != "linear_lambdarank" || len(explanation.Results) != 2 {
		t.Fatalf("explanation = %#v", explanation)
	}
	result := explanation.Results[0]
	if result.FinalRank != 1 || result.URL != "https://example.test/alpha" ||
		result.RetrievalDiagnostic != "retrieval evidence" || result.Learned == nil ||
		len(result.Learned.Signals) == 0 {
		t.Fatalf("result = %#v", result)
	}
	if len(result.FieldContributions) != 2 ||
		result.FieldContributions[0].Name != "body" ||
		result.FieldContributions[1].Name != "title" {
		t.Fatalf("field contributions = %#v", result.FieldContributions)
	}
	strictRank := false
	for _, signal := range result.Evidence {
		strictRank = strictRank || signal.Name == "strict_rank" && signal.Value == 1
	}
	if !strictRank {
		t.Fatalf("ranking evidence = %#v", result.Evidence)
	}

	withoutModel, err := newSearchExplainEndpoint(index, nil, nil, nil, nil).
		Explain(t.Context(), "alpha", false)
	if err != nil || withoutModel.Results[0].Learned != nil {
		t.Fatalf("unlearned explanation = %#v, %v", withoutModel, err)
	}
}

func TestSearchExplainEndpointBacksGlobalConsoleAndSurfacesFailure(t *testing.T) {
	global := &globalSearchExplainScript{response: searchcore.Response{
		Results: []searchcore.Result{{URL: "https://global.example/"}},
	}}
	explanation, err := newSearchExplainEndpoint(nil, nil, nil, nil, nil).
		withGlobal(global).
		Explain(t.Context(), "alpha", true)
	if err != nil || !explanation.Global || len(explanation.Results) != 1 {
		t.Fatalf("global explanation = %#v, %v", explanation, err)
	}
	retrievalFailure := errors.New("global retrieval failed")
	_, err = newSearchExplainEndpoint(nil, nil, nil, nil, nil).
		withGlobal(&globalSearchExplainScript{err: retrievalFailure}).
		Explain(t.Context(), "alpha", true)
	if !errors.Is(err, retrievalFailure) {
		t.Fatalf("global retrieval error = %v", err)
	}
}

func TestAdminSearchPartialFailuresUseHumanWebLabel(t *testing.T) {
	failures := adminSearchPartialFailures([]searchcore.PartialFailure{{
		Source: searchcore.PartialFailureSourceWeb,
		Reason: "provider failed",
	}})
	if len(failures) != 1 || failures[0] != "web: provider failed" {
		t.Fatalf("failures = %#v", failures)
	}
}

func TestAdminSearchLearnedTreeConversion(t *testing.T) {
	explanation := &learnedrank.ResultExplanation{Trees: []learnedrank.TreeExplanation{{
		TreeIndex: 2, InteractionGroup: "lexical", Contribution: 0.5,
		Decisions: []learnedrank.TreeDecision{{
			Name: "title_score", Known: true, TerminatedMissing: false,
			NormalizedValue: 0.7, Threshold: 0.4, WentLeft: true,
		}},
	}}}
	converted := adminSearchLearnedExplanation(explanation)
	if converted == nil || len(converted.Trees) != 1 {
		t.Fatalf("learned explanation = %#v", converted)
	}
	tree := converted.Trees[0]
	if tree.Index != 2 || tree.InteractionGroup != "lexical" ||
		tree.Contribution != 0.5 || len(tree.Decisions) != 1 ||
		!tree.Decisions[0].WentLeft {
		t.Fatalf("tree = %#v", tree)
	}
	if adminSearchLearnedExplanation(nil) != nil {
		t.Fatal("nil learned explanation should remain absent")
	}
}
