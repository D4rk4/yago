package searchindex

import (
	"math"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestRankingWeightsValidate(t *testing.T) {
	if err := DefaultRankingWeights().Validate(); err != nil {
		t.Fatalf("default weights invalid: %v", err)
	}

	cases := []struct {
		name    string
		weights RankingWeights
	}{
		{"negative", RankingWeights{Title: -1, Body: 1}},
		{"nan", RankingWeights{Title: math.NaN(), Body: 1}},
		{"inf", RankingWeights{Title: math.Inf(1), Body: 1}},
		{"all zero", RankingWeights{}},
	}
	for _, tc := range cases {
		if err := tc.weights.Validate(); err == nil {
			t.Fatalf("%s weights: expected a validation error", tc.name)
		}
	}
}

func TestRankingWeightsOrDefault(t *testing.T) {
	if got := (RankingWeights{}).orDefault(); got != DefaultRankingWeights() {
		t.Fatalf("zero orDefault = %#v, want default", got)
	}
	custom := RankingWeights{Title: 2, Body: 1}
	if got := custom.orDefault(); got != custom {
		t.Fatalf("custom orDefault = %#v, want %#v", got, custom)
	}
}

func TestSearchWeightsChangeRankingAndExplain(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{
			{NormalizedURL: "https://a.example/", Title: "alpha", ExtractedText: "filler body"},
			{
				NormalizedURL: "https://b.example/",
				Title:         "filler",
				ExtractedText: "alpha in the body",
			},
		},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	titleHeavy, err := index.Search(t.Context(), SearchRequest{
		Query:      "alpha",
		MaxResults: 5,
		Explain:    true,
		Weights:    RankingWeights{Title: 8, Headings: 1, Anchors: 1, Body: 1, URL: 1},
	})
	if err != nil {
		t.Fatalf("title-heavy search: %v", err)
	}
	if len(titleHeavy.Results) != 2 || titleHeavy.Results[0].URL != "https://a.example/" {
		t.Fatalf("title-heavy results = %#v, want a.example first", titleHeavy.Results)
	}
	if titleHeavy.Results[0].Explanation == "" {
		t.Fatal("explain requested but explanation is empty")
	}

	bodyHeavy, err := index.Search(t.Context(), SearchRequest{
		Query:      "alpha",
		MaxResults: 5,
		Weights:    RankingWeights{Title: 1, Headings: 1, Anchors: 1, Body: 8, URL: 1},
	})
	if err != nil {
		t.Fatalf("body-heavy search: %v", err)
	}
	if bodyHeavy.Results[0].URL != "https://b.example/" {
		t.Fatalf("body-heavy top = %q, want b.example", bodyHeavy.Results[0].URL)
	}
	if bodyHeavy.Results[0].Explanation != "" {
		t.Fatal("explanation should be empty when explain is not requested")
	}
}
