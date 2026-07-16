package learnedrank

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestLearnedLocalOrderKeepsFederatedSlotRelevanceScale(t *testing.T) {
	ranker, err := NewRanker(4)
	if err != nil {
		t.Fatal(err)
	}
	if err := ranker.Activate(mustSnapshot(
		t,
		"mixed-source",
		mustLinearModel(t, linearWeights(map[int]float64{0: 0.1})),
	)); err != nil {
		t.Fatal(err)
	}
	localOriginalTop := searchcore.WithDiversityRelevance(
		rankingResult("https://local.example/original", 1, 0.016),
		0.9,
	)
	localOriginalTop.Source = searchcore.SourceGlobal
	localOriginalTop.Title = "original local discussion"
	web := searchcore.WithDiversityRelevance(searchcore.Result{
		URL:    "https://web.example/archive",
		Title:  "external web archive",
		Source: searchcore.SourceWeb,
		Score:  0.016,
	}, 0.85)
	localModelTop := searchcore.WithDiversityRelevance(
		rankingResult("https://local.example/model", 3, 0.015),
		0.8,
	)
	localModelTop.Source = searchcore.SourceGlobal
	localModelTop.Title = "model selected local community"

	outcome, err := ranker.Rerank(
		searchcore.Request{Source: searchcore.SourceGlobal},
		[]searchcore.Result{localOriginalTop, web, localModelTop},
	)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Results[0].URL != localModelTop.URL ||
		outcome.Results[1].URL != web.URL ||
		outcome.Results[2].URL != localOriginalTop.URL {
		t.Fatalf("learned order = %v", resultURLs(outcome.Results))
	}
	diversified := searchcore.DiversifyResults(
		outcome.Results,
		searchcore.Request{Source: searchcore.SourceGlobal},
	)
	if diversified[0].URL != localModelTop.URL {
		t.Fatalf("diversified order = %v", resultURLs(diversified))
	}
}
