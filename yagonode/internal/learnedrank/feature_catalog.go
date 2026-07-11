package learnedrank

import (
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/rankfit"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type rankingFeature struct {
	signal    searchcore.RankingSignal
	direction rankfit.FeatureDirection
}

const rankingFeatureCount = 33

var rankingFeatures = [rankingFeatureCount]rankingFeature{
	{searchcore.SignalRetrievalScore, rankfit.FeatureIncreasing},
	{searchcore.SignalStrictScore, rankfit.FeatureIncreasing},
	{searchcore.SignalStrictRank, rankfit.FeatureDecreasing},
	{searchcore.SignalRelaxedScore, rankfit.FeatureIncreasing},
	{searchcore.SignalRelaxedRank, rankfit.FeatureDecreasing},
	{searchcore.SignalFeedbackScore, rankfit.FeatureIncreasing},
	{searchcore.SignalFeedbackRank, rankfit.FeatureDecreasing},
	{searchcore.SignalTitleScore, rankfit.FeatureIncreasing},
	{searchcore.SignalHeadingScore, rankfit.FeatureIncreasing},
	{searchcore.SignalAnchorScore, rankfit.FeatureIncreasing},
	{searchcore.SignalURLScore, rankfit.FeatureIncreasing},
	{searchcore.SignalBodyScore, rankfit.FeatureIncreasing},
	{searchcore.SignalTermCoverage, rankfit.FeatureIncreasing},
	{searchcore.SignalOrderedProximity, rankfit.FeatureIncreasing},
	{searchcore.SignalUnorderedProximity, rankfit.FeatureIncreasing},
	{searchcore.SignalGlobalProximity, rankfit.FeatureIncreasing},
	{searchcore.SignalQuality, rankfit.FeatureIncreasing},
	{searchcore.SignalQualityKnown, rankfit.FeatureUnconstrained},
	{searchcore.SignalSpamRisk, rankfit.FeatureDecreasing},
	{searchcore.SignalFunctionWordFraction, rankfit.FeatureUnconstrained},
	{searchcore.SignalSymbolFraction, rankfit.FeatureUnconstrained},
	{searchcore.SignalAlphabeticFraction, rankfit.FeatureUnconstrained},
	{searchcore.SignalUniqueTokenFraction, rankfit.FeatureUnconstrained},
	{searchcore.SignalDateConfidence, rankfit.FeatureUnconstrained},
	{searchcore.SignalFreshness, rankfit.FeatureIncreasing},
	{searchcore.SignalAuthority, rankfit.FeatureIncreasing},
	{searchcore.SignalAuthorityConfidence, rankfit.FeatureUnconstrained},
	{searchcore.SignalURLPrior, rankfit.FeatureIncreasing},
	{searchcore.SignalLocalRank, rankfit.FeatureDecreasing},
	{searchcore.SignalRemoteRank, rankfit.FeatureDecreasing},
	{searchcore.SignalPeerSupport, rankfit.FeatureIncreasing},
	{searchcore.SignalPeerReputation, rankfit.FeatureIncreasing},
	{searchcore.SignalSourceCount, rankfit.FeatureIncreasing},
}

func FeatureDefinitions() []rankfit.FeatureDefinition {
	definitions := make([]rankfit.FeatureDefinition, len(rankingFeatures))
	for index, feature := range rankingFeatures {
		definitions[index] = rankfit.FeatureDefinition{
			Name:      feature.signal.Name(),
			Direction: feature.direction,
		}
	}

	return definitions
}

func MapRankingEvidence(
	evidence searchcore.RankingEvidence,
) (rankfit.FeatureVector, bool, error) {
	values := make([]float64, len(rankingFeatures))
	knownValues := make([]bool, len(rankingFeatures))
	knownSignals := 0
	for index, feature := range rankingFeatures {
		value, known := evidence.Value(feature.signal)
		if known {
			values[index] = value
			knownValues[index] = true
			knownSignals++
		}
	}
	if knownSignals != len(evidence.Values()) {
		return rankfit.FeatureVector{}, false, fmt.Errorf(
			"ranking evidence contains a signal outside the feature catalog",
		)
	}
	vector, err := rankfit.NewFeatureVectorWithKnownValues(values, knownValues)
	if err != nil {
		return rankfit.FeatureVector{}, false, fmt.Errorf("map ranking evidence: %w", err)
	}

	return vector, knownSignals > 0, nil
}

func rankingFeatureIndex(name string) (int, bool) {
	for index, feature := range rankingFeatures {
		if feature.signal.Name() == name {
			return index, true
		}
	}

	return 0, false
}
