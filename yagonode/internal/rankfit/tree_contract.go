package rankfit

import (
	"fmt"
	"math"
	"slices"
)

const (
	maximumHistogramTrees               = 64
	maximumHistogramDepth               = 4
	maximumHistogramBins                = 32
	maximumHistogramL2Regularization    = 1e6
	maximumHistogramMinimumLeafExamples = 2048
	maximumHistogramLeafMagnitude       = 1e6
	minimumHistogramGain                = 1e-12
)

type FeatureInteractionGroup struct {
	Name           string `json:"name"`
	FeatureIndices []int  `json:"feature_indices"`
}

type HistogramLambdaMARTTrainingOptions struct {
	MaximumTrees                             int
	MaximumDepth                             int
	MaximumBins                              int
	LearningRate                             float64
	L2Regularization                         float64
	MinimumLeafExamples                      int
	NormalizedDiscountedCumulativeGainCutoff int
	FeatureInteractionGroups                 []FeatureInteractionGroup
}

type HistogramLambdaMARTTrainingReport struct {
	Trees           int
	PreferencePairs int
}

func DefaultHistogramLambdaMARTTrainingOptions() HistogramLambdaMARTTrainingOptions {
	return HistogramLambdaMARTTrainingOptions{
		MaximumTrees:                             maximumHistogramTrees,
		MaximumDepth:                             maximumHistogramDepth,
		MaximumBins:                              32,
		LearningRate:                             0.05,
		L2Regularization:                         1,
		MinimumLeafExamples:                      5,
		NormalizedDiscountedCumulativeGainCutoff: 10,
	}
}

func (o HistogramLambdaMARTTrainingOptions) validatedInteractionGroups(
	featureDefinitions []FeatureDefinition,
) ([]FeatureInteractionGroup, error) {
	if err := o.validateBounds(); err != nil {
		return nil, err
	}
	groups := o.FeatureInteractionGroups
	if len(groups) == 0 {
		groups = singletonFeatureInteractionGroups(featureDefinitions)
	}

	return canonicalFeatureInteractionGroups(groups, len(featureDefinitions))
}

func (o HistogramLambdaMARTTrainingOptions) validateBounds() error {
	if o.MaximumTrees < 1 || o.MaximumTrees > maximumHistogramTrees {
		return fmt.Errorf("maximum trees must be between 1 and %d", maximumHistogramTrees)
	}
	if o.MaximumDepth < 1 || o.MaximumDepth > maximumHistogramDepth {
		return fmt.Errorf("maximum depth must be between 1 and %d", maximumHistogramDepth)
	}
	if o.MaximumBins < 2 || o.MaximumBins > maximumHistogramBins {
		return fmt.Errorf("maximum bins must be between 2 and %d", maximumHistogramBins)
	}
	if !finiteHistogramValue(o.LearningRate, 1, false) {
		return fmt.Errorf("learning rate must be finite and in (0, 1]")
	}
	if !finiteHistogramValue(o.L2Regularization, maximumHistogramL2Regularization, true) {
		return fmt.Errorf("L2 regularization must be finite and bounded")
	}
	if o.MinimumLeafExamples < 1 ||
		o.MinimumLeafExamples > maximumHistogramMinimumLeafExamples {
		return fmt.Errorf(
			"minimum leaf examples must be between 1 and %d",
			maximumHistogramMinimumLeafExamples,
		)
	}
	if o.NormalizedDiscountedCumulativeGainCutoff < 1 ||
		o.NormalizedDiscountedCumulativeGainCutoff > maximumLinearExamplesPerQuery {
		return fmt.Errorf(
			"normalized discounted cumulative gain cutoff must be between 1 and %d",
			maximumLinearExamplesPerQuery,
		)
	}

	return nil
}

func finiteHistogramValue(value, upper float64, includeZero bool) bool {
	if math.IsNaN(value) || math.IsInf(value, 0) || value > upper {
		return false
	}
	if includeZero {
		return value >= 0
	}

	return value > 0
}

func singletonFeatureInteractionGroups(
	featureDefinitions []FeatureDefinition,
) []FeatureInteractionGroup {
	groups := make([]FeatureInteractionGroup, len(featureDefinitions))
	for index, definition := range featureDefinitions {
		groups[index] = FeatureInteractionGroup{
			Name:           definition.Name,
			FeatureIndices: []int{index},
		}
	}

	return groups
}

func canonicalFeatureInteractionGroups(
	groups []FeatureInteractionGroup,
	dimension int,
) ([]FeatureInteractionGroup, error) {
	if len(groups) == 0 || len(groups) > maximumLinearFeatures {
		return nil, fmt.Errorf(
			"feature interaction groups must be between 1 and %d",
			maximumLinearFeatures,
		)
	}
	canonical := make([]FeatureInteractionGroup, len(groups))
	for index, group := range groups {
		validated, err := canonicalFeatureInteractionGroup(group, dimension)
		if err != nil {
			return nil, err
		}
		canonical[index] = validated
	}
	slices.SortFunc(canonical, func(left, right FeatureInteractionGroup) int {
		return compareIdentifiers(left.Name, right.Name)
	})
	for index := 1; index < len(canonical); index++ {
		if canonical[index-1].Name == canonical[index].Name {
			return nil, fmt.Errorf(
				"feature interaction group %q is duplicated",
				canonical[index].Name,
			)
		}
	}

	return canonical, nil
}

func canonicalFeatureInteractionGroup(
	group FeatureInteractionGroup,
	dimension int,
) (FeatureInteractionGroup, error) {
	if group.Name == "" || !validFeatureName(group.Name) {
		return FeatureInteractionGroup{}, fmt.Errorf("feature interaction group name is invalid")
	}
	if len(group.FeatureIndices) == 0 || len(group.FeatureIndices) > dimension {
		return FeatureInteractionGroup{}, fmt.Errorf(
			"feature interaction group %q has an invalid size",
			group.Name,
		)
	}
	indices := append([]int(nil), group.FeatureIndices...)
	slices.Sort(indices)
	for index, feature := range indices {
		if feature < 0 || feature >= dimension {
			return FeatureInteractionGroup{}, fmt.Errorf(
				"feature interaction group %q has an invalid feature index",
				group.Name,
			)
		}
		if index > 0 && indices[index-1] == feature {
			return FeatureInteractionGroup{}, fmt.Errorf(
				"feature interaction group %q repeats a feature",
				group.Name,
			)
		}
	}

	return FeatureInteractionGroup{Name: group.Name, FeatureIndices: indices}, nil
}
