package rankfit

import (
	"context"
	"fmt"
)

type histogramTrainingInputs struct {
	groups            []normalizedQueryGroup
	interactionGroups []FeatureInteractionGroup
	set               histogramTrainingSet
}

func TrainHistogramLambdaMART(
	ctx context.Context,
	featureDefinitions []FeatureDefinition,
	queryGroups []QueryGroup,
	options HistogramLambdaMARTTrainingOptions,
) (HistogramLambdaMARTModel, HistogramLambdaMARTTrainingReport, error) {
	inputs, err := validatedHistogramTrainingInputs(
		ctx,
		featureDefinitions,
		queryGroups,
		options,
	)
	if err != nil {
		return HistogramLambdaMARTModel{}, HistogramLambdaMARTTrainingReport{}, err
	}
	scores := newHistogramScoreMatrix(inputs.groups)
	trees := make([]histogramRankingTree, 0, options.MaximumTrees)
	report := HistogramLambdaMARTTrainingReport{}
	for range options.MaximumTrees {
		if err := ctx.Err(); err != nil {
			return HistogramLambdaMARTModel{}, HistogramLambdaMARTTrainingReport{},
				fmt.Errorf("train histogram LambdaMART: %w", err)
		}
		derivatives, err := histogramLambdaDerivativesForGroups(
			ctx,
			inputs.groups,
			scores,
			options.NormalizedDiscountedCumulativeGainCutoff,
		)
		if err != nil {
			return HistogramLambdaMARTModel{}, HistogramLambdaMARTTrainingReport{},
				fmt.Errorf("train histogram LambdaMART: %w", err)
		}
		report.PreferencePairs = derivatives.preferencePairs
		if derivatives.preferencePairs == 0 {
			break
		}
		tree, found, err := bestHistogramRankingTree(
			ctx,
			inputs,
			derivatives,
			featureDefinitions,
			options,
		)
		if err != nil {
			return HistogramLambdaMARTModel{}, HistogramLambdaMARTTrainingReport{},
				fmt.Errorf("train histogram LambdaMART: %w", err)
		}
		if !found {
			break
		}
		trees = append(trees, tree)
		updateHistogramScores(scores, inputs.set, tree, options.LearningRate)
	}
	report.Trees = len(trees)
	model := HistogramLambdaMARTModel{
		featureDefinitions: append([]FeatureDefinition(nil), featureDefinitions...),
		learningRate:       options.LearningRate,
		trees:              cloneHistogramRankingTrees(trees),
	}

	return model, report, model.Validate()
}

func validatedHistogramTrainingInputs(
	ctx context.Context,
	featureDefinitions []FeatureDefinition,
	queryGroups []QueryGroup,
	options HistogramLambdaMARTTrainingOptions,
) (histogramTrainingInputs, error) {
	if ctx == nil {
		return histogramTrainingInputs{}, fmt.Errorf("training context must not be nil")
	}
	if err := validateFeatureDefinitions(featureDefinitions); err != nil {
		return histogramTrainingInputs{}, err
	}
	if len(queryGroups) == 0 || len(queryGroups) > maximumLinearQueries {
		return histogramTrainingInputs{}, fmt.Errorf(
			"training query groups must be between 1 and %d",
			maximumLinearQueries,
		)
	}
	interactionGroups, err := options.validatedInteractionGroups(featureDefinitions)
	if err != nil {
		return histogramTrainingInputs{}, err
	}
	if err := ctx.Err(); err != nil {
		return histogramTrainingInputs{}, fmt.Errorf("train histogram LambdaMART: %w", err)
	}
	if err := validateTrainingWork(ctx, queryGroups, len(featureDefinitions)); err != nil {
		return histogramTrainingInputs{}, err
	}
	groups, err := normalizeTrainingGroups(ctx, queryGroups, len(featureDefinitions))
	if err != nil {
		return histogramTrainingInputs{}, err
	}

	return histogramTrainingInputs{
		groups:            groups,
		interactionGroups: interactionGroups,
		set: newHistogramTrainingSet(
			groups,
			len(featureDefinitions),
			options.MaximumBins,
		),
	}, nil
}

func bestHistogramRankingTree(
	ctx context.Context,
	inputs histogramTrainingInputs,
	derivatives histogramLambdaDerivatives,
	featureDefinitions []FeatureDefinition,
	options HistogramLambdaMARTTrainingOptions,
) (histogramRankingTree, bool, error) {
	best := histogramRankingTree{}
	bestGain := 0.0
	found := false
	for _, group := range inputs.interactionGroups {
		builder := histogramTreeBuilder{
			ctx:                ctx,
			set:                inputs.set,
			derivatives:        derivatives,
			featureDefinitions: featureDefinitions,
			interactionGroup:   group,
			options:            options,
		}
		tree, gain, split, err := builder.build()
		if err != nil {
			return histogramRankingTree{}, false, err
		}
		if split && (!found || gain > bestGain+minimumHistogramGain) {
			best = tree
			bestGain = gain
			found = true
		}
	}

	return best, found, nil
}

func updateHistogramScores(
	scores [][]float64,
	set histogramTrainingSet,
	tree histogramRankingTree,
	learningRate float64,
) {
	for _, row := range set.rows {
		value, _ := tree.evaluate(row.values, nil, false)
		scores[row.queryIndex][row.exampleIndex] += learningRate * value
	}
}
