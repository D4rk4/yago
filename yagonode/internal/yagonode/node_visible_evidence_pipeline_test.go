package yagonode

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestRankingPipelineAnalyzesPeerWebAndLegacyVisibleEvidence(t *testing.T) {
	results := []searchcore.Result{
		{Source: searchcore.SourceRemote, Language: "ru", Snippet: "Чрезвычайных полномочий"},
		{Source: searchcore.SourceWeb, Language: "ru", Snippet: "Чрезвычайных полномочий"},
		{Source: searchcore.SourceGlobal, Language: "ru", Snippet: "Чрезвычайных полномочий"},
	}
	searcher := assembleRankingStages(
		staticSearcher{resp: searchcore.Response{Results: results}},
		publicSearchAssembly{},
	)
	response, err := searcher.Search(t.Context(), searchcore.Request{
		Terms: []string{"чрезвычайные", "полномочия"},
		Limit: len(results),
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(response.Results) != len(results) {
		t.Fatalf("results = %d", len(response.Results))
	}
	for index, result := range response.Results {
		if !result.EvidenceReady || result.Analyzer != "ru" ||
			len(result.QueryMatches) != 2 {
			t.Fatalf("result %d = %#v", index, result)
		}
	}
}
