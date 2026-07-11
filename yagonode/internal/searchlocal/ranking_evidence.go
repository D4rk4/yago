package searchlocal

import (
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func localRankingEvidence(
	req searchcore.Request,
	result searchindex.SearchResult,
) searchcore.RankingEvidence {
	evidence := searchcore.NewRankingEvidence(
		searchcore.RankingSignalValue{Signal: searchcore.SignalRetrievalScore, Value: result.Score},
		searchcore.RankingSignalValue{
			Signal: searchcore.SignalURLPrior,
			Value:  urlLengthPrior(result.URL),
		},
		searchcore.RankingSignalValue{
			Signal: searchcore.SignalQualityKnown,
			Value:  boolRankingValue(result.QualityKnown),
		},
	)
	if result.StrictRank > 0 {
		evidence = evidence.With(searchcore.SignalStrictRank, float64(result.StrictRank))
		evidence = evidence.With(searchcore.SignalStrictScore, result.StrictScore)
	}
	if result.RelaxedRank > 0 {
		evidence = evidence.With(searchcore.SignalRelaxedRank, float64(result.RelaxedRank))
		evidence = evidence.With(searchcore.SignalRelaxedScore, result.RelaxedScore)
	}
	if result.QualityKnown {
		evidence = evidence.With(searchcore.SignalQuality, result.Quality)
		evidence = evidence.With(searchcore.SignalSpamRisk, result.SpamRisk)
		evidence = evidence.With(
			searchcore.SignalFunctionWordFraction,
			result.FunctionWordFraction,
		)
		evidence = evidence.With(searchcore.SignalSymbolFraction, result.SymbolFraction)
		evidence = evidence.With(
			searchcore.SignalAlphabeticFraction,
			result.AlphabeticFraction,
		)
		evidence = evidence.With(
			searchcore.SignalUniqueTokenFraction,
			result.UniqueTokenFraction,
		)
	}
	if result.DateConfidence > 0 {
		evidence = evidence.With(searchcore.SignalDateConfidence, result.DateConfidence)
	}
	if len(req.Terms) > 1 {
		evidence = evidence.With(searchcore.SignalUnorderedProximity, result.Proximity)
		evidence = evidence.With(
			searchcore.SignalOrderedProximity,
			result.OrderedProximity,
		)
	}
	for field, score := range result.FieldScores {
		signal, ok := fieldRankingSignal(field)
		if ok {
			evidence = evidence.With(signal, score)
		}
	}

	return evidence
}

func fieldRankingSignal(field string) (searchcore.RankingSignal, bool) {
	switch field {
	case "title":
		return searchcore.SignalTitleScore, true
	case "headings":
		return searchcore.SignalHeadingScore, true
	case "anchors":
		return searchcore.SignalAnchorScore, true
	case "url":
		return searchcore.SignalURLScore, true
	case "body":
		return searchcore.SignalBodyScore, true
	default:
		return 0, false
	}
}

func boolRankingValue(value bool) float64 {
	if value {
		return 1
	}

	return 0
}
