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
	// HostRank scales the local host-authority boost (YBR-style block rank) folded
	// into a result's score after retrieval. It is a post-retrieval multiplier, not
	// a text-field boost, so it does not count toward the relevance-weight
	// requirement below. Zero (the default) disables the host-authority signal.
	HostRank float64 `json:"hostRank"`
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
	if math.IsNaN(w.HostRank) || math.IsInf(w.HostRank, 0) {
		return fmt.Errorf("ranking weight hostRank must be a finite number")
	}
	if w.HostRank < 0 {
		return fmt.Errorf("ranking weight hostRank must not be negative")
	}

	return nil
}

func (w RankingWeights) orDefault() RankingWeights {
	if w == (RankingWeights{}) {
		return DefaultRankingWeights()
	}

	return w
}
