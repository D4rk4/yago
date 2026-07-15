package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchsession"
)

type liveRankingResultSource struct{}

func (liveRankingResultSource) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	return searchcore.Response{Request: req, TotalResults: 3, Results: []searchcore.Result{
		{
			URL:   "https://retrieval.example/",
			Host:  "retrieval.example",
			Score: 1,
			Title: "unrelated",
		},
		{URL: "https://lexical.example/", Host: "lexical.example", Score: 0.9, Title: "alpha beta"},
		{URL: "https://tail.example/", Host: "tail.example", Score: 0.8, Title: "alpha"},
	}}, nil
}

func TestRankingProfileSetChangesNextFirstPageSearch(t *testing.T) {
	holder := testRankingHolder(t)
	weights := holder.Current()
	weights.LexicalBlend = 0
	if err := holder.Set(t.Context(), weights); err != nil {
		t.Fatalf("set retrieval profile: %v", err)
	}
	stable := searchsession.NewStableWindow(assembleRankingStages(
		liveRankingResultSource{},
		publicSearchAssembly{rankingWeights: holder.Current},
	))
	request := searchcore.Request{
		Query: "alpha beta", Terms: []string{"alpha", "beta"}, Limit: 3,
	}
	first, err := stable.Search(t.Context(), request)
	if err != nil {
		t.Fatalf("first search: %v", err)
	}
	if first.Results[0].URL != "https://retrieval.example/" {
		t.Fatalf("first order = %+v", first.Results)
	}
	weights.LexicalBlend = 1
	if err := holder.Set(t.Context(), weights); err != nil {
		t.Fatalf("set lexical profile: %v", err)
	}
	second, err := stable.Search(t.Context(), request)
	if err != nil {
		t.Fatalf("second search: %v", err)
	}
	if second.Results[0].URL != "https://lexical.example/" {
		t.Fatalf("second order = %+v", second.Results)
	}
}
