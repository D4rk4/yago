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
	URLPrior float64 `json:"urlPrior"`
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
	// Proximity scales the SDM unordered-window feature folded into a result's
	// score after retrieval: a page where adjacent query words cluster within a
	// small token window gains up to Proximity×proximity(text) over one that merely
	// mentions the words apart. It completes the Sequential Dependence Model beside
	// the ordered bigram boost that rides the query, and like the other priors it
	// is a post-retrieval multiplier that a pre-existing profile decodes as zero.
	Proximity           float64 `json:"proximity"`
	OrderedProximity    float64 `json:"orderedProximity"`
	LexicalBlend        float64 `json:"lexicalBlend"`
	LexicalGapAgreement float64 `json:"lexicalGapAgreement"`
}

func DefaultRankingWeights() RankingWeights {
	var weights RankingWeights
	for _, definition := range rankingWeightDefinitions {
		weights.Set(definition.Key, definition.Default)
	}

	return weights
}

func (w RankingWeights) Validate() error {
	return w.validate(true)
}

func (w RankingWeights) ValidatePersisted() error {
	return w.validate(false)
}

func (w RankingWeights) validate(enforceMaximum bool) error {
	positive := false
	for _, definition := range rankingWeightDefinitions {
		value, _ := w.Value(definition.Key)
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("ranking weight %s must be a finite number", definition.Key)
		}
		if value < definition.Minimum {
			return fmt.Errorf(
				"ranking weight %s must be at least %g",
				definition.Key,
				definition.Minimum,
			)
		}
		if enforceMaximum && value > definition.Maximum {
			return fmt.Errorf(
				"ranking weight %s must be between %g and %g",
				definition.Key,
				definition.Minimum,
				definition.Maximum,
			)
		}
		if definition.FieldBoost && value > 0 {
			positive = true
		}
	}
	if !positive {
		return fmt.Errorf("at least one field boost must be positive")
	}

	return nil
}

func (w RankingWeights) orDefault() RankingWeights {
	if w == (RankingWeights{}) {
		return DefaultRankingWeights()
	}

	return w
}
