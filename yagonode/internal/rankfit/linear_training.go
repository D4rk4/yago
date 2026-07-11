package rankfit

import (
	"context"
	"fmt"
	"math"
	"slices"
)

const maximumLinearTrainingIterations = 10000

type LinearLambdaRankTrainingOptions struct {
	LearningRate                             float64
	Regularization                           float64
	MaximumIterations                        int
	NormalizedDiscountedCumulativeGainCutoff int
	MinimumWeightChange                      float64
	MaximumAbsoluteWeight                    float64
}

type LinearLambdaRankTrainingReport struct {
	Iterations      int
	PreferencePairs int
}

func DefaultLinearLambdaRankTrainingOptions() LinearLambdaRankTrainingOptions {
	return LinearLambdaRankTrainingOptions{
		LearningRate:                             0.05,
		Regularization:                           0.001,
		MaximumIterations:                        200,
		NormalizedDiscountedCumulativeGainCutoff: 10,
		MinimumWeightChange:                      1e-6,
		MaximumAbsoluteWeight:                    8,
	}
}

func TrainLinearLambdaRank(
	ctx context.Context,
	featureDefinitions []FeatureDefinition,
	queryGroups []QueryGroup,
	options LinearLambdaRankTrainingOptions,
) (LinearLambdaRankModel, LinearLambdaRankTrainingReport, error) {
	if ctx == nil {
		return LinearLambdaRankModel{}, LinearLambdaRankTrainingReport{}, fmt.Errorf(
			"training context must not be nil",
		)
	}
	if err := validateFeatureDefinitions(featureDefinitions); err != nil {
		return LinearLambdaRankModel{}, LinearLambdaRankTrainingReport{}, err
	}
	if len(queryGroups) == 0 || len(queryGroups) > maximumLinearQueries {
		return LinearLambdaRankModel{}, LinearLambdaRankTrainingReport{}, fmt.Errorf(
			"training query groups must be between 1 and %d",
			maximumLinearQueries,
		)
	}
	if err := options.validate(); err != nil {
		return LinearLambdaRankModel{}, LinearLambdaRankTrainingReport{}, err
	}

	if err := validateTrainingWork(ctx, queryGroups, len(featureDefinitions)); err != nil {
		return LinearLambdaRankModel{}, LinearLambdaRankTrainingReport{}, err
	}
	normalizedGroups, err := normalizeTrainingGroups(ctx, queryGroups, len(featureDefinitions))
	if err != nil {
		return LinearLambdaRankModel{}, LinearLambdaRankTrainingReport{}, err
	}
	weights := make([]float64, len(featureDefinitions))
	report := LinearLambdaRankTrainingReport{}
	for iteration := range options.MaximumIterations {
		if err := ctx.Err(); err != nil {
			return LinearLambdaRankModel{}, LinearLambdaRankTrainingReport{}, fmt.Errorf(
				"train linear LambdaRank: %w",
				err,
			)
		}
		gradient, preferencePairs, err := lambdaGradient(
			ctx,
			normalizedGroups,
			weights,
			options.NormalizedDiscountedCumulativeGainCutoff,
		)
		if err != nil {
			return LinearLambdaRankModel{}, LinearLambdaRankTrainingReport{}, fmt.Errorf(
				"train linear LambdaRank: %w",
				err,
			)
		}
		maximumChange := updateLinearWeights(weights, gradient, featureDefinitions, options)
		report.Iterations = iteration + 1
		report.PreferencePairs = preferencePairs
		if maximumChange <= options.MinimumWeightChange {
			break
		}
	}

	return LinearLambdaRankModel{
		featureDefinitions: append([]FeatureDefinition(nil), featureDefinitions...),
		weights:            weights,
		missingPolicy:      missingEvidenceNeutral,
	}, report, nil
}

func (o LinearLambdaRankTrainingOptions) validate() error {
	if !finiteWithin(o.LearningRate, 1, false) {
		return fmt.Errorf("learning rate must be finite and in (0, 1]")
	}
	if !finiteWithin(o.Regularization, 1, true) {
		return fmt.Errorf("regularization must be finite and in [0, 1]")
	}
	if o.MaximumIterations < 1 || o.MaximumIterations > maximumLinearTrainingIterations {
		return fmt.Errorf(
			"maximum iterations must be between 1 and %d",
			maximumLinearTrainingIterations,
		)
	}
	if o.NormalizedDiscountedCumulativeGainCutoff < 1 ||
		o.NormalizedDiscountedCumulativeGainCutoff > maximumLinearExamplesPerQuery {
		return fmt.Errorf(
			"normalized discounted cumulative gain cutoff must be between 1 and %d",
			maximumLinearExamplesPerQuery,
		)
	}
	if !finiteWithin(o.MinimumWeightChange, 1, true) {
		return fmt.Errorf("minimum weight change must be finite and in [0, 1]")
	}
	if !finiteWithin(o.MaximumAbsoluteWeight, maximumLinearWeightMagnitude, false) {
		return fmt.Errorf("maximum absolute weight must be finite and in (0, 1000000]")
	}

	return nil
}

func finiteWithin(value, upper float64, includeZero bool) bool {
	if math.IsNaN(value) || math.IsInf(value, 0) || value > upper {
		return false
	}
	if includeZero {
		return value >= 0
	}

	return value > 0
}

func normalizeTrainingGroups(
	ctx context.Context,
	queryGroups []QueryGroup,
	dimension int,
) ([]normalizedQueryGroup, error) {
	normalized := make([]normalizedQueryGroup, len(queryGroups))
	for index, group := range queryGroups {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("normalize training queries: %w", err)
		}
		var err error
		normalized[index], err = normalizeQueryGroup(
			group,
			dimension,
			missingEvidenceNeutral,
		)
		if err != nil {
			return nil, fmt.Errorf("normalize training query: %w", err)
		}
	}

	return normalized, nil
}

func lambdaGradient(
	ctx context.Context,
	groups []normalizedQueryGroup,
	weights []float64,
	cutoff int,
) ([]float64, int, error) {
	gradient := make([]float64, len(weights))
	activeQueries := 0
	preferencePairs := 0
	for _, group := range groups {
		queryGradient, pairs, err := queryLambdaGradient(ctx, group, weights, cutoff)
		if err != nil {
			return nil, 0, err
		}
		preferencePairs += pairs
		if pairs == 0 {
			continue
		}
		activeQueries++
		for feature := range gradient {
			gradient[feature] += queryGradient[feature] / float64(pairs)
		}
	}
	if activeQueries > 0 {
		for feature := range gradient {
			gradient[feature] /= float64(activeQueries)
		}
	}

	return gradient, preferencePairs, nil
}

func queryLambdaGradient(
	ctx context.Context,
	group normalizedQueryGroup,
	weights []float64,
	cutoff int,
) ([]float64, int, error) {
	scores, err := linearQueryScores(ctx, group.examples, weights)
	if err != nil {
		return nil, 0, err
	}
	positions := scorePositions(group.examples, scores)
	ideal := idealDiscountedCumulativeGain(group.examples, cutoff)
	gradient := make([]float64, len(weights))
	if ideal == 0 {
		return gradient, 0, nil
	}
	pairs, err := accumulateLinearQueryLambdas(
		ctx,
		group.examples,
		scores,
		discountedGainContext{positions: positions, cutoff: cutoff, ideal: ideal},
		gradient,
	)
	if err != nil {
		return nil, 0, err
	}

	return gradient, pairs, nil
}

func linearQueryScores(
	ctx context.Context,
	examples []normalizedRankingExample,
	weights []float64,
) ([]float64, error) {
	scores := make([]float64, len(examples))
	for index, example := range examples {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("compute linear lambdas: %w", err)
		}
		for feature, weight := range weights {
			scores[index] += weight * example.values[feature]
		}
	}

	return scores, nil
}

func accumulateLinearQueryLambdas(
	ctx context.Context,
	examples []normalizedRankingExample,
	scores []float64,
	discount discountedGainContext,
	gradient []float64,
) (int, error) {
	pairs := 0
	accumulator := linearLambdaAccumulator{
		examples: examples,
		scores:   scores,
		discount: discount,
		gradient: gradient,
	}
	for left := range examples {
		if err := ctx.Err(); err != nil {
			return 0, fmt.Errorf("compute linear lambdas: %w", err)
		}
		for right := left + 1; right < len(examples); right++ {
			if right&63 == 0 {
				if err := ctx.Err(); err != nil {
					return 0, fmt.Errorf("compute linear lambdas: %w", err)
				}
			}
			if accumulator.add(left, right) {
				pairs++
			}
		}
	}

	return pairs, nil
}

type linearLambdaAccumulator struct {
	examples []normalizedRankingExample
	scores   []float64
	discount discountedGainContext
	gradient []float64
}

func (accumulator linearLambdaAccumulator) add(left, right int) bool {
	preferred, other, distinct := preferredPair(accumulator.examples, left, right)
	if !distinct {
		return false
	}
	delta := normalizedDiscountedCumulativeGainSwap(
		accumulator.examples,
		preferred,
		other,
		accumulator.discount,
	)
	if delta == 0 {
		return false
	}
	lambda := delta * preferredPairProbability(
		accumulator.scores[preferred]-accumulator.scores[other],
	)
	for feature := range accumulator.gradient {
		accumulator.gradient[feature] += lambda *
			(accumulator.examples[preferred].values[feature] -
				accumulator.examples[other].values[feature])
	}

	return true
}

func scorePositions(examples []normalizedRankingExample, scores []float64) []int {
	indices := make([]int, len(examples))
	for index := range indices {
		indices[index] = index
	}
	slices.SortStableFunc(indices, func(left, right int) int {
		if scores[left] > scores[right] {
			return -1
		}
		if scores[left] < scores[right] {
			return 1
		}

		return 0
	})
	positions := make([]int, len(examples))
	for position, index := range indices {
		positions[index] = position
	}

	return positions
}

func preferredPair(examples []normalizedRankingExample, left, right int) (int, int, bool) {
	if examples[left].relevance > examples[right].relevance {
		return left, right, true
	}
	if examples[right].relevance > examples[left].relevance {
		return right, left, true
	}

	return 0, 0, false
}

func idealDiscountedCumulativeGain(examples []normalizedRankingExample, cutoff int) float64 {
	relevances := make([]int, len(examples))
	for index, example := range examples {
		relevances[index] = example.relevance
	}
	slices.SortFunc(relevances, func(left, right int) int { return right - left })
	gain := 0.0
	for position, relevance := range relevances {
		gain += relevanceGain(relevance) * rankDiscount(position, cutoff)
	}

	return gain
}

type discountedGainContext struct {
	positions []int
	cutoff    int
	ideal     float64
}

func normalizedDiscountedCumulativeGainSwap(
	examples []normalizedRankingExample,
	preferred int,
	other int,
	context discountedGainContext,
) float64 {
	gainDifference := relevanceGain(examples[preferred].relevance) -
		relevanceGain(examples[other].relevance)
	discountDifference := rankDiscount(context.positions[preferred], context.cutoff) -
		rankDiscount(context.positions[other], context.cutoff)

	return math.Abs(gainDifference*discountDifference) / context.ideal
}

func relevanceGain(relevance int) float64 {
	return math.Exp2(float64(relevance)) - 1
}

func rankDiscount(position, cutoff int) float64 {
	if position >= cutoff {
		return 0
	}

	return 1 / math.Log2(float64(position)+2)
}

func preferredPairProbability(scoreDifference float64) float64 {
	if scoreDifference >= 0 {
		exponential := math.Exp(-scoreDifference)

		return exponential / (1 + exponential)
	}
	exponential := math.Exp(scoreDifference)

	return 1 / (1 + exponential)
}

func updateLinearWeights(
	weights []float64,
	gradient []float64,
	featureDefinitions []FeatureDefinition,
	options LinearLambdaRankTrainingOptions,
) float64 {
	maximumChange := 0.0
	for feature := range weights {
		updated := weights[feature] +
			options.LearningRate*(gradient[feature]-options.Regularization*weights[feature])
		updated = max(-options.MaximumAbsoluteWeight, min(updated, options.MaximumAbsoluteWeight))
		if featureDefinitions[feature].Direction == FeatureIncreasing {
			updated = max(0, updated)
		}
		if featureDefinitions[feature].Direction == FeatureDecreasing {
			updated = min(0, updated)
		}
		maximumChange = max(maximumChange, math.Abs(updated-weights[feature]))
		weights[feature] = updated
	}

	return maximumChange
}
