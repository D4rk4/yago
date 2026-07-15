package searchlocal

import (
	"time"

	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func (h *hostRankScorer) authorityPrior(result *searchcore.Result) float64 {
	if h.hostWeight <= 0 && !h.authorityEvidence || h.table == nil {
		return 0
	}
	authority, known := h.table[hostrank.RegistrableDomain(result.URL)]
	if !known {
		return 0
	}
	result.Evidence = result.Evidence.With(searchcore.SignalAuthority, authority.Score)
	result.Evidence = result.Evidence.With(
		searchcore.SignalAuthorityConfidence,
		authority.Confidence,
	)
	if h.hostWeight <= 0 {
		return 0
	}

	return h.hostWeight * authority.Score * authority.Confidence
}

func (h *hostRankScorer) freshnessPrior(result *searchcore.Result) float64 {
	if h.freshWeight <= 0 && !h.freshnessEvidence {
		return 0
	}
	published, err := time.Parse("20060102", result.Date)
	if err != nil || result.DateConfidence <= 0 {
		return 0
	}
	age := h.now.Sub(published)
	if age < 0 {
		age = 0
	}
	freshness := result.DateConfidence * h.freshness.Score(age)
	result.Evidence = result.Evidence.With(searchcore.SignalFreshness, freshness)

	return h.freshWeight * freshness
}
