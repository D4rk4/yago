package learnedrank

import (
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/rankfit"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func (s Snapshot) evaluate(
	group rankfit.QueryGroup,
	candidates []rankingCandidate,
	explain bool,
) ([]candidateEvaluation, error) {
	switch s.kind {
	case ModelLinearLambdaRank:
		return s.evaluateLinear(group, candidates, explain)
	case ModelHistogramLambdaMART:
		return s.evaluateHistogram(group, candidates, explain)
	default:
		return nil, fmt.Errorf("model kind %q is unsupported", s.kind)
	}
}

func (s Snapshot) evaluateLinear(
	group rankfit.QueryGroup,
	candidates []rankingCandidate,
	explain bool,
) ([]candidateEvaluation, error) {
	if !explain {
		predictions, err := s.linear.Predict(group)
		if err != nil {
			return nil, fmt.Errorf("run linear LambdaRank model: %w", err)
		}

		return rankedDocumentEvaluations(predictions, len(candidates))
	}
	explanations, err := s.linear.Explain(group)
	if err != nil {
		return nil, fmt.Errorf("explain linear LambdaRank model: %w", err)
	}
	byIdentifier := candidateEvidence(candidates)
	evaluations := make([]candidateEvaluation, len(explanations))
	for index, explanation := range explanations {
		candidate, exists := byIdentifier[explanation.DocumentIdentifier]
		if !exists {
			return nil, fmt.Errorf("linear LambdaRank explanation references an unknown document")
		}
		signals := linearSignalExplanations(
			candidate.result.Evidence,
			explanation.FeatureContributions,
		)
		evaluations[index] = candidateEvaluation{
			documentIdentifier: explanation.DocumentIdentifier,
			score:              explanation.Score,
			rank:               explanation.Rank,
			signals:            signals,
		}
	}

	return validateEvaluations(evaluations, len(candidates))
}

func (s Snapshot) evaluateHistogram(
	group rankfit.QueryGroup,
	candidates []rankingCandidate,
	explain bool,
) ([]candidateEvaluation, error) {
	if !explain {
		predictions, err := s.histogram.Predict(group)
		if err != nil {
			return nil, fmt.Errorf("run histogram LambdaMART model: %w", err)
		}

		return rankedDocumentEvaluations(predictions, len(candidates))
	}
	explanations, err := s.histogram.Explain(group)
	if err != nil {
		return nil, fmt.Errorf("explain histogram LambdaMART model: %w", err)
	}
	byIdentifier := candidateEvidence(candidates)
	evaluations := make([]candidateEvaluation, len(explanations))
	for index, explanation := range explanations {
		candidate, exists := byIdentifier[explanation.DocumentIdentifier]
		if !exists {
			return nil, fmt.Errorf(
				"histogram LambdaMART explanation references an unknown document",
			)
		}
		signals := rawSignalExplanations(candidate.result.Evidence)
		trees := treeExplanations(explanation.TreeContributions, signals)
		evaluations[index] = candidateEvaluation{
			documentIdentifier: explanation.DocumentIdentifier,
			score:              explanation.Score,
			rank:               explanation.Rank,
			signals:            signals,
			trees:              trees,
		}
	}

	return validateEvaluations(evaluations, len(candidates))
}

func rankedDocumentEvaluations(
	predictions []rankfit.RankedDocument,
	expected int,
) ([]candidateEvaluation, error) {
	evaluations := make([]candidateEvaluation, len(predictions))
	for index, prediction := range predictions {
		evaluations[index] = candidateEvaluation{
			documentIdentifier: prediction.DocumentIdentifier,
			score:              prediction.Score,
			rank:               prediction.Rank,
		}
	}

	return validateEvaluations(evaluations, expected)
}

func validateEvaluations(
	evaluations []candidateEvaluation,
	expected int,
) ([]candidateEvaluation, error) {
	if len(evaluations) != expected {
		return nil, fmt.Errorf(
			"learned ranking model returned %d documents, expected %d",
			len(evaluations),
			expected,
		)
	}
	seen := make(map[string]struct{}, len(evaluations))
	for _, evaluation := range evaluations {
		if evaluation.documentIdentifier == "" {
			return nil, fmt.Errorf("learned ranking model returned an empty document identifier")
		}
		if _, exists := seen[evaluation.documentIdentifier]; exists {
			return nil, fmt.Errorf("learned ranking model returned a document more than once")
		}
		seen[evaluation.documentIdentifier] = struct{}{}
	}

	return evaluations, nil
}

func candidateEvidence(candidates []rankingCandidate) map[string]rankingCandidate {
	byIdentifier := make(map[string]rankingCandidate, len(candidates))
	for _, candidate := range candidates {
		byIdentifier[candidate.documentIdentifier] = candidate
	}

	return byIdentifier
}

func rawSignalExplanations(
	evidence searchcore.RankingEvidence,
) []SignalExplanation {
	signals := make([]SignalExplanation, len(rankingFeatures))
	for index, feature := range rankingFeatures {
		value, known := evidence.Value(feature.signal)
		signals[index] = SignalExplanation{
			Signal: feature.signal,
			Name:   feature.signal.Name(),
			Known:  known,
			Value:  value,
		}
	}

	return signals
}

func linearSignalExplanations(
	evidence searchcore.RankingEvidence,
	contributions []rankfit.FeatureContribution,
) []SignalExplanation {
	signals := rawSignalExplanations(evidence)
	for index, contribution := range contributions {
		signals[index].Used = true
		signals[index].NormalizedValue = contribution.NormalizedValue
		signals[index].Weight = contribution.Weight
		signals[index].Contribution = contribution.Contribution
	}

	return signals
}

func treeExplanations(
	contributions []rankfit.HistogramTreeContribution,
	signals []SignalExplanation,
) []TreeExplanation {
	trees := make([]TreeExplanation, len(contributions))
	for index, contribution := range contributions {
		decisions := make([]TreeDecision, len(contribution.Decisions))
		for decisionIndex, decision := range contribution.Decisions {
			featureIndex, _ := rankingFeatureIndex(decision.FeatureName)
			signals[featureIndex].Used = true
			signals[featureIndex].NormalizedValue = decision.Value
			decisions[decisionIndex] = TreeDecision{
				Signal:          rankingFeatures[featureIndex].signal,
				Name:            decision.FeatureName,
				NormalizedValue: decision.Value,
				Threshold:       decision.Threshold,
				WentLeft:        decision.WentLeft,
			}
		}
		trees[index] = TreeExplanation{
			TreeIndex:        contribution.TreeIndex,
			InteractionGroup: contribution.InteractionGroup,
			Contribution:     contribution.Contribution,
			Decisions:        decisions,
		}
	}

	return trees
}
