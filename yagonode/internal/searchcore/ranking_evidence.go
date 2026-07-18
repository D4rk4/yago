package searchcore

import "math"

type RankingSignal uint8

const (
	SignalRetrievalScore RankingSignal = iota
	SignalStrictScore
	SignalStrictRank
	SignalRelaxedScore
	SignalRelaxedRank
	SignalFeedbackScore
	SignalFeedbackRank
	SignalTitleScore
	SignalHeadingScore
	SignalAnchorScore
	SignalURLScore
	SignalBodyScore
	SignalTermCoverage
	SignalOrderedProximity
	SignalUnorderedProximity
	SignalGlobalProximity
	SignalQuality
	SignalQualityKnown
	SignalSpamRisk
	SignalFunctionWordFraction
	SignalSymbolFraction
	SignalAlphabeticFraction
	SignalUniqueTokenFraction
	SignalDateConfidence
	SignalFreshness
	SignalAuthority
	SignalAuthorityConfidence
	SignalURLPrior
	SignalLocalRank
	SignalRemoteRank
	SignalPeerSupport
	SignalPeerReputation
	SignalSourceCount
	SignalWebRank
	rankingSignalLimit
)

type RankingSignalValue struct {
	Signal RankingSignal
	Value  float64
}

type RankingEvidence struct {
	values [rankingSignalLimit]float64
	known  uint64
}

func NewRankingEvidence(values ...RankingSignalValue) RankingEvidence {
	var evidence RankingEvidence
	for _, value := range values {
		evidence = evidence.With(value.Signal, value.Value)
	}

	return evidence
}

func (e RankingEvidence) With(signal RankingSignal, value float64) RankingEvidence {
	if signal >= rankingSignalLimit || math.IsNaN(value) || math.IsInf(value, 0) {
		return e
	}
	e.values[signal] = value
	e.known |= uint64(1) << signal

	return e
}

func (e RankingEvidence) Add(signal RankingSignal, value float64) RankingEvidence {
	if current, known := e.Value(signal); known {
		value += current
	}

	return e.With(signal, value)
}

func (e RankingEvidence) Value(signal RankingSignal) (float64, bool) {
	if signal >= rankingSignalLimit || e.known&(uint64(1)<<signal) == 0 {
		return 0, false
	}

	return e.values[signal], true
}

func (e RankingEvidence) Values() []RankingSignalValue {
	values := make([]RankingSignalValue, 0, rankingSignalLimit)
	for signal := RankingSignal(0); signal < rankingSignalLimit; signal++ {
		if value, known := e.Value(signal); known {
			values = append(values, RankingSignalValue{Signal: signal, Value: value})
		}
	}

	return values
}

func (e RankingEvidence) Overlay(other RankingEvidence) RankingEvidence {
	for _, value := range other.Values() {
		if _, known := e.Value(value.Signal); !known {
			e = e.With(value.Signal, value.Value)
		}
	}

	return e
}

func (s RankingSignal) Name() string {
	if s >= rankingSignalLimit {
		return ""
	}

	return rankingSignalNames[s]
}

var rankingSignalNames = [...]string{
	"retrieval_score",
	"strict_score",
	"strict_rank",
	"relaxed_score",
	"relaxed_rank",
	"feedback_score",
	"feedback_rank",
	"title_score",
	"heading_score",
	"anchor_score",
	"url_score",
	"body_score",
	"term_coverage",
	"ordered_proximity",
	"unordered_proximity",
	"global_proximity",
	"quality",
	"quality_known",
	"spam_risk",
	"function_word_fraction",
	"symbol_fraction",
	"alphabetic_fraction",
	"unique_token_fraction",
	"date_confidence",
	"freshness",
	"authority",
	"authority_confidence",
	"url_prior",
	"local_rank",
	"remote_rank",
	"peer_support",
	"peer_reputation",
	"source_count",
	"web_rank",
}
