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
	// requirement below. Zero disables the host-authority signal.
	HostRank float64 `json:"hostRank"`
	// Freshness scales the recency prior folded into a result's score after
	// retrieval: a dated document gains up to Freshness×exp(−ln2·age/half-life)
	// on top of its relevance, so newer pages win ties without burying the
	// archive (undated documents keep their score). Zero disables it.
	Freshness float64 `json:"freshness"`
	// Quality scales the deterministic content-quality prior (contentprior) folded
	// into a result's score after retrieval: a clean, prose-like page gains up to
	// Quality×quality(text) over a keyword-stuffed one. It is a post-retrieval
	// multiplier, not a text-field boost. Zero disables it. A ranking profile
	// persisted before this weight existed decodes it as zero, so the quality
	// prior stays off until the profile is re-saved or re-tuned.
	Quality float64 `json:"quality"`
}

func DefaultRankingWeights() RankingWeights {
	// Text-field weights follow BM25F practice (title ≫ headings ≫ anchors ≫
	// body); the post-retrieval priors default ON — host authority (YBR) and a
	// gentle freshness decay — because relevance alone cannot break ties in a
	// small federated corpus (SEARCH-38).
	// Field weights sit in the practical BM25F range from the TREC-13 web
	// track (title and anchor text far above body; Robertson & Zaragoza,
	// CIKM 2004): anchor text is what OTHER pages call this one — for
	// navigational queries it outranks the page's own body by an order of
	// magnitude in tuned systems.
	return RankingWeights{
		Title:     6,
		Headings:  3,
		Anchors:   4,
		Body:      1,
		URL:       2,
		HostRank:  0.3,
		Freshness: 0.2,
		Quality:   0.2,
	}
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
	for _, prior := range []struct {
		name  string
		value float64
	}{{"hostRank", w.HostRank}, {"freshness", w.Freshness}, {"quality", w.Quality}} {
		if math.IsNaN(prior.value) || math.IsInf(prior.value, 0) {
			return fmt.Errorf("ranking weight %s must be a finite number", prior.name)
		}
		if prior.value < 0 {
			return fmt.Errorf("ranking weight %s must not be negative", prior.name)
		}
	}

	return nil
}

func (w RankingWeights) orDefault() RankingWeights {
	if w == (RankingWeights{}) {
		return DefaultRankingWeights()
	}

	return w
}
