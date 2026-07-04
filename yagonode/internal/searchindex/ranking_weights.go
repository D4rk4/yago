package searchindex

import (
	"fmt"
	"math"
)

type RankingWeights struct {
	Title    float64 `json:"title"`
	Headings float64 `json:"headings"`
	Anchors  float64 `json:"anchors"`
	Body     float64 `json:"body"`
	URL      float64 `json:"url"`
}

func DefaultRankingWeights() RankingWeights {
	return RankingWeights{Title: 4, Headings: 3, Anchors: 2, Body: 1, URL: 1}
}

func (w RankingWeights) Validate() error {
	fields := []struct {
		name  string
		value float64
	}{
		{"title", w.Title},
		{"headings", w.Headings},
		{"anchors", w.Anchors},
		{"body", w.Body},
		{"url", w.URL},
	}
	positive := false
	for _, field := range fields {
		if math.IsNaN(field.value) || math.IsInf(field.value, 0) {
			return fmt.Errorf("ranking weight %s must be a finite number", field.name)
		}
		if field.value < 0 {
			return fmt.Errorf("ranking weight %s must not be negative", field.name)
		}
		if field.value > 0 {
			positive = true
		}
	}
	if !positive {
		return fmt.Errorf("at least one ranking weight must be positive")
	}

	return nil
}

func (w RankingWeights) orDefault() RankingWeights {
	if w == (RankingWeights{}) {
		return DefaultRankingWeights()
	}

	return w
}
