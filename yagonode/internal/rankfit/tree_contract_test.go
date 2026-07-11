package rankfit

import (
	"math"
	"reflect"
	"testing"
)

func TestHistogramTrainingDefaultsAndSingletonGroups(t *testing.T) {
	options := DefaultHistogramLambdaMARTTrainingOptions()
	definitions := definitionsForTest("zeta", "alpha")
	groups, err := options.validatedInteractionGroups(definitions)
	if err != nil {
		t.Fatalf("validatedInteractionGroups: %v", err)
	}
	want := []FeatureInteractionGroup{
		{Name: "alpha", FeatureIndices: []int{1}},
		{Name: "zeta", FeatureIndices: []int{0}},
	}
	if !reflect.DeepEqual(groups, want) {
		t.Fatalf("groups = %#v, want %#v", groups, want)
	}
	if options.MaximumTrees != maximumHistogramTrees ||
		options.MaximumDepth != maximumHistogramDepth ||
		options.MaximumBins != 32 || options.LearningRate != 0.05 ||
		options.L2Regularization != 1 || options.MinimumLeafExamples != 5 ||
		options.NormalizedDiscountedCumulativeGainCutoff != 10 {
		t.Fatalf("defaults = %#v", options)
	}
}

func TestHistogramInteractionGroupsAreCanonicalAndImmutable(t *testing.T) {
	options := DefaultHistogramLambdaMARTTrainingOptions()
	options.FeatureInteractionGroups = []FeatureInteractionGroup{
		{Name: "zeta", FeatureIndices: []int{2, 0}},
		{Name: "alpha", FeatureIndices: []int{1}},
	}
	groups, err := options.validatedInteractionGroups(definitionsForTest("a", "b", "c"))
	if err != nil {
		t.Fatalf("validatedInteractionGroups: %v", err)
	}
	options.FeatureInteractionGroups[0].FeatureIndices[0] = 1
	want := []FeatureInteractionGroup{
		{Name: "alpha", FeatureIndices: []int{1}},
		{Name: "zeta", FeatureIndices: []int{0, 2}},
	}
	if !reflect.DeepEqual(groups, want) {
		t.Fatalf("groups = %#v, want %#v", groups, want)
	}
}

func TestHistogramTrainingOptionBounds(t *testing.T) {
	valid := DefaultHistogramLambdaMARTTrainingOptions()
	cases := []HistogramLambdaMARTTrainingOptions{
		withHistogramOption(valid, func(value *HistogramLambdaMARTTrainingOptions) {
			value.MaximumTrees = 0
		}),
		withHistogramOption(valid, func(value *HistogramLambdaMARTTrainingOptions) {
			value.MaximumTrees = maximumHistogramTrees + 1
		}),
		withHistogramOption(valid, func(value *HistogramLambdaMARTTrainingOptions) {
			value.MaximumDepth = 0
		}),
		withHistogramOption(valid, func(value *HistogramLambdaMARTTrainingOptions) {
			value.MaximumDepth = maximumHistogramDepth + 1
		}),
		withHistogramOption(valid, func(value *HistogramLambdaMARTTrainingOptions) {
			value.MaximumBins = 1
		}),
		withHistogramOption(valid, func(value *HistogramLambdaMARTTrainingOptions) {
			value.MaximumBins = maximumHistogramBins + 1
		}),
		withHistogramOption(valid, func(value *HistogramLambdaMARTTrainingOptions) {
			value.LearningRate = math.NaN()
		}),
		withHistogramOption(valid, func(value *HistogramLambdaMARTTrainingOptions) {
			value.LearningRate = 2
		}),
		withHistogramOption(valid, func(value *HistogramLambdaMARTTrainingOptions) {
			value.L2Regularization = -1
		}),
		withHistogramOption(valid, func(value *HistogramLambdaMARTTrainingOptions) {
			value.L2Regularization = math.Inf(1)
		}),
		withHistogramOption(valid, func(value *HistogramLambdaMARTTrainingOptions) {
			value.MinimumLeafExamples = 0
		}),
		withHistogramOption(valid, func(value *HistogramLambdaMARTTrainingOptions) {
			value.MinimumLeafExamples = maximumHistogramMinimumLeafExamples + 1
		}),
		withHistogramOption(valid, func(value *HistogramLambdaMARTTrainingOptions) {
			value.NormalizedDiscountedCumulativeGainCutoff = 0
		}),
		withHistogramOption(valid, func(value *HistogramLambdaMARTTrainingOptions) {
			value.NormalizedDiscountedCumulativeGainCutoff = maximumLinearExamplesPerQuery + 1
		}),
	}
	for _, options := range cases {
		if _, err := options.validatedInteractionGroups(definitionsForTest("feature")); err == nil {
			t.Errorf("validatedInteractionGroups accepted %#v", options)
		}
	}
	if !finiteHistogramValue(0, 1, true) || finiteHistogramValue(-1, 1, true) ||
		!finiteHistogramValue(1, 1, false) || finiteHistogramValue(0, 1, false) {
		t.Fatalf("finiteHistogramValue bounds are incorrect")
	}
}

func TestHistogramInteractionGroupValidation(t *testing.T) {
	tooManyGroups := make([]FeatureInteractionGroup, maximumLinearFeatures+1)
	tooManyIndices := make([]int, 3)
	cases := [][]FeatureInteractionGroup{
		nil,
		tooManyGroups,
		{{Name: "", FeatureIndices: []int{0}}},
		{{Name: "bad name", FeatureIndices: []int{0}}},
		{{Name: "empty"}},
		{{Name: "large", FeatureIndices: tooManyIndices}},
		{{Name: "negative", FeatureIndices: []int{-1}}},
		{{Name: "outside", FeatureIndices: []int{2}}},
		{{Name: "repeat", FeatureIndices: []int{0, 0}}},
		{{Name: "same", FeatureIndices: []int{0}}, {Name: "same", FeatureIndices: []int{1}}},
	}
	for _, groups := range cases {
		if _, err := canonicalFeatureInteractionGroups(groups, 2); err == nil {
			t.Errorf("canonicalFeatureInteractionGroups accepted %#v", groups)
		}
	}
	options := DefaultHistogramLambdaMARTTrainingOptions()
	if _, err := options.validatedInteractionGroups(nil); err == nil {
		t.Errorf("validatedInteractionGroups accepted no feature definitions")
	}
}

func withHistogramOption(
	options HistogramLambdaMARTTrainingOptions,
	change func(*HistogramLambdaMARTTrainingOptions),
) HistogramLambdaMARTTrainingOptions {
	change(&options)

	return options
}
