package rankfit

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
)

const (
	linearLambdaRankLegacyFormat = "yago-linear-lambdarank-v1"
	linearLambdaRankFormat       = "yago-linear-lambdarank-v2"
)

type LinearLambdaRankModel struct {
	featureDefinitions []FeatureDefinition
	weights            []float64
	missingPolicy      missingEvidencePolicy
}

type RankedDocument struct {
	DocumentIdentifier string
	Score              float64
	Rank               int
}

type FeatureContribution struct {
	FeatureName     string
	Known           bool
	Used            bool
	NormalizedValue float64
	Weight          float64
	Contribution    float64
}

type RankingExplanation struct {
	DocumentIdentifier   string
	Score                float64
	Rank                 int
	FeatureContributions []FeatureContribution
}

type linearLambdaRankModelDocument struct {
	Format             string              `json:"format"`
	FeatureDefinitions []FeatureDefinition `json:"features"`
	Weights            []float64           `json:"weights"`
}

func NewLinearLambdaRankModel(
	featureDefinitions []FeatureDefinition,
	weights []float64,
) (LinearLambdaRankModel, error) {
	return newLinearLambdaRankModel(featureDefinitions, weights, missingEvidenceNeutral)
}

func newLinearLambdaRankModel(
	featureDefinitions []FeatureDefinition,
	weights []float64,
	missingPolicy missingEvidencePolicy,
) (LinearLambdaRankModel, error) {
	if err := validateFeatureDefinitions(featureDefinitions); err != nil {
		return LinearLambdaRankModel{}, err
	}
	if !missingPolicy.valid() {
		return LinearLambdaRankModel{}, fmt.Errorf("missing evidence policy is invalid")
	}
	if len(weights) != len(featureDefinitions) {
		return LinearLambdaRankModel{}, dimensionMismatchError(
			len(featureDefinitions),
			len(weights),
		)
	}
	for index, weight := range weights {
		if math.IsNaN(weight) || math.IsInf(weight, 0) {
			return LinearLambdaRankModel{}, fmt.Errorf("model weights must be finite")
		}
		if math.Abs(weight) > maximumLinearWeightMagnitude {
			return LinearLambdaRankModel{}, fmt.Errorf("model weights must be bounded")
		}
		direction := featureDefinitions[index].Direction
		if direction == FeatureIncreasing && weight < 0 ||
			direction == FeatureDecreasing && weight > 0 {
			return LinearLambdaRankModel{}, fmt.Errorf(
				"feature %q weight violates its direction",
				featureDefinitions[index].Name,
			)
		}
	}

	return LinearLambdaRankModel{
		featureDefinitions: append([]FeatureDefinition(nil), featureDefinitions...),
		weights:            append([]float64(nil), weights...),
		missingPolicy:      missingPolicy,
	}, nil
}

func (m LinearLambdaRankModel) FeatureDefinitions() []FeatureDefinition {
	return append([]FeatureDefinition(nil), m.featureDefinitions...)
}

func (m LinearLambdaRankModel) Weights() []float64 {
	return append([]float64(nil), m.weights...)
}

func (m LinearLambdaRankModel) Validate() error {
	_, err := newLinearLambdaRankModel(m.featureDefinitions, m.weights, m.missingPolicy)

	return err
}

func (m LinearLambdaRankModel) Predict(group QueryGroup) ([]RankedDocument, error) {
	normalized, err := m.normalizedGroup(group)
	if err != nil {
		return nil, err
	}
	evaluations := m.evaluate(normalized)
	predictions := make([]RankedDocument, len(evaluations))
	for index, evaluation := range evaluations {
		predictions[index] = RankedDocument{
			DocumentIdentifier: evaluation.documentIdentifier,
			Score:              evaluation.score,
			Rank:               index + 1,
		}
	}

	return predictions, nil
}

func (m LinearLambdaRankModel) Explain(group QueryGroup) ([]RankingExplanation, error) {
	normalized, err := m.normalizedGroup(group)
	if err != nil {
		return nil, err
	}
	evaluations := m.evaluate(normalized)
	explanations := make([]RankingExplanation, len(evaluations))
	for index, evaluation := range evaluations {
		contributions := make([]FeatureContribution, len(m.weights))
		for feature, weight := range m.weights {
			value := evaluation.values[feature]
			known := evaluation.known[feature]
			contributions[feature] = FeatureContribution{
				FeatureName:     m.featureDefinitions[feature].Name,
				Known:           known,
				Used:            known || m.missingPolicy == missingEvidenceAsObservedZero,
				NormalizedValue: value,
				Weight:          weight,
				Contribution:    value * weight,
			}
		}
		explanations[index] = RankingExplanation{
			DocumentIdentifier:   evaluation.documentIdentifier,
			Score:                evaluation.score,
			Rank:                 index + 1,
			FeatureContributions: contributions,
		}
	}

	return explanations, nil
}

func (m LinearLambdaRankModel) MarshalJSON() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	encoded := append([]byte(`{"format":`), strconv.Quote(linearModelFormat(m.missingPolicy))...)
	encoded = append(encoded, `,"features":[`...)
	for index, definition := range m.featureDefinitions {
		if index > 0 {
			encoded = append(encoded, ',')
		}
		encoded = append(encoded, `{"name":`...)
		encoded = append(encoded, strconv.Quote(definition.Name)...)
		encoded = append(encoded, `,"direction":`...)
		encoded = strconv.AppendInt(encoded, int64(definition.Direction), 10)
		encoded = append(encoded, '}')
	}
	encoded = append(encoded, `],"weights":[`...)
	for index, weight := range m.weights {
		if index > 0 {
			encoded = append(encoded, ',')
		}
		encoded = strconv.AppendFloat(encoded, weight, 'g', -1, 64)
	}
	encoded = append(encoded, ']', '}')

	return encoded, nil
}

func (m *LinearLambdaRankModel) UnmarshalJSON(data []byte) error {
	var document linearLambdaRankModelDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("decode linear LambdaRank model: %w", err)
	}
	missingPolicy, valid := linearMissingEvidencePolicy(document.Format)
	if !valid {
		return fmt.Errorf("unsupported linear LambdaRank model format %q", document.Format)
	}
	model, err := newLinearLambdaRankModel(
		document.FeatureDefinitions,
		document.Weights,
		missingPolicy,
	)
	if err != nil {
		return fmt.Errorf("validate linear LambdaRank model: %w", err)
	}
	*m = model

	return nil
}

type linearEvaluation struct {
	documentIdentifier string
	values             []float64
	known              []bool
	score              float64
}

func (m LinearLambdaRankModel) normalizedGroup(group QueryGroup) (normalizedQueryGroup, error) {
	if err := m.Validate(); err != nil {
		return normalizedQueryGroup{}, err
	}

	return normalizeQueryGroup(group, len(m.weights), m.missingPolicy)
}

func (m LinearLambdaRankModel) evaluate(group normalizedQueryGroup) []linearEvaluation {
	evaluations := make([]linearEvaluation, len(group.examples))
	for index, example := range group.examples {
		score := 0.0
		for feature, weight := range m.weights {
			score += weight * example.values[feature]
		}
		evaluations[index] = linearEvaluation{
			documentIdentifier: example.documentIdentifier,
			values:             example.values,
			known:              example.known,
			score:              score,
		}
	}
	slices.SortStableFunc(evaluations, func(left, right linearEvaluation) int {
		if left.score > right.score {
			return -1
		}
		if left.score < right.score {
			return 1
		}

		return 0
	})

	return evaluations
}

func linearModelFormat(policy missingEvidencePolicy) string {
	if policy == missingEvidenceAsObservedZero {
		return linearLambdaRankLegacyFormat
	}

	return linearLambdaRankFormat
}

func linearMissingEvidencePolicy(format string) (missingEvidencePolicy, bool) {
	switch format {
	case linearLambdaRankLegacyFormat:
		return missingEvidenceAsObservedZero, true
	case linearLambdaRankFormat:
		return missingEvidenceNeutral, true
	default:
		return 0, false
	}
}

func compareIdentifiers(left, right string) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}

	return 0
}

func dimensionMismatchError(expected, actual int) error {
	return fmt.Errorf("feature dimension is %d, expected %d", actual, expected)
}
