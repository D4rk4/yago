package rankfit

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strconv"
	"testing"
)

func TestRobustQueryNormalization(t *testing.T) {
	center, scale := robustCenterAndScale([]float64{0, 1, 2, 3})
	if center != 1.5 || scale != 1.5 {
		t.Fatalf("center, scale = %v, %v", center, scale)
	}
	center, scale = robustCenterAndScale([]float64{0, 0, 0, 0, 10})
	if center != 0 || scale != 10 {
		t.Fatalf("range fallback center, scale = %v, %v", center, scale)
	}
	center, scale = robustCenterAndScale([]float64{4})
	if center != 4 || scale != 1 {
		t.Fatalf("constant fallback center, scale = %v, %v", center, scale)
	}
	if quantile([]float64{0, 4}, 0.25) != 1 {
		t.Fatalf("interpolated quantile is incorrect")
	}

	group := mustQueryGroup(
		t,
		"outlier",
		mustRankingExample(t, "a", 0, -1),
		mustRankingExample(t, "b", 0, 0),
		mustRankingExample(t, "c", 0, 0),
		mustRankingExample(t, "d", 0, 0),
		mustRankingExample(t, "e", 0, 1),
		mustRankingExample(t, "f", 0, 100),
	)
	normalized, err := normalizeQueryGroup(group, 1, missingEvidenceNeutral)
	if err != nil {
		t.Fatalf("normalizeQueryGroup: %v", err)
	}
	if normalized.queryIdentifier != "outlier" ||
		normalized.examples[len(normalized.examples)-1].values[0] != maximumNormalizedFeatureMagnitude {
		t.Fatalf("normalized outlier was not bounded: %#v", normalized)
	}
	if _, err := normalizeQueryGroup(QueryGroup{}, 1, missingEvidenceNeutral); err == nil {
		t.Errorf("normalizeQueryGroup accepted an invalid group")
	}
	if _, err := normalizeQueryGroup(group, 2, missingEvidenceNeutral); err == nil {
		t.Errorf("normalizeQueryGroup accepted the wrong dimension")
	}
	if _, err := normalizeQueryGroup(group, 1, 0); err == nil {
		t.Errorf("normalizeQueryGroup accepted an invalid missing policy")
	}

	sparse := mustQueryGroup(
		t,
		"sparse",
		mustKnownRankingExample(t, "low", []float64{0, 0}, []bool{true, false}),
		mustKnownRankingExample(t, "missing", []float64{0, 0}, []bool{false, false}),
		mustKnownRankingExample(t, "high", []float64{2, 0}, []bool{true, false}),
	)
	normalized, err = normalizeQueryGroup(sparse, 2, missingEvidenceNeutral)
	if err != nil {
		t.Fatalf("normalize sparse query: %v", err)
	}
	if normalized.examples[0].values[0] != -1 ||
		normalized.examples[1].values[0] != 0 ||
		normalized.examples[2].values[0] != 1 ||
		normalized.examples[1].known[0] || normalized.examples[0].known[1] {
		t.Fatalf("sparse normalization = %#v", normalized.examples)
	}
}

func TestLinearLambdaRankTrainingLearnsSignedModel(t *testing.T) {
	definitions := []FeatureDefinition{
		{Name: "quality", Direction: FeatureIncreasing},
		{Name: "risk", Direction: FeatureDecreasing},
		{Name: "coverage", Direction: FeatureUnconstrained},
	}
	group := mustQueryGroup(
		t,
		"learn",
		mustRankingExample(t, "best", 3, 2, 0, 2),
		mustRankingExample(t, "middle", 1, 1, 1, 1),
		mustRankingExample(t, "worst", 0, 0, 2, 0),
	)
	options := DefaultLinearLambdaRankTrainingOptions()
	options.MaximumIterations = 20
	options.MinimumWeightChange = 0
	options.MaximumAbsoluteWeight = 0.25
	model, report, err := TrainLinearLambdaRank(
		t.Context(),
		definitions,
		[]QueryGroup{group},
		options,
	)
	if err != nil {
		t.Fatalf("TrainLinearLambdaRank: %v", err)
	}
	weights := model.Weights()
	if weights[0] <= 0 || weights[1] >= 0 || weights[2] <= 0 {
		t.Fatalf("signed weights = %v", weights)
	}
	for _, weight := range weights {
		if math.Abs(weight) > options.MaximumAbsoluteWeight {
			t.Fatalf("weight %v exceeded bound", weight)
		}
	}
	if report.Iterations != options.MaximumIterations || report.PreferencePairs != 3 {
		t.Fatalf("report = %#v", report)
	}
	predictions, err := model.Predict(group)
	if err != nil {
		t.Fatalf("Predict: %v", err)
	}
	if predictions[0].DocumentIdentifier != "best" || predictions[2].DocumentIdentifier != "worst" {
		t.Fatalf("trained prediction order = %#v", predictions)
	}

	zeroGroup := mustQueryGroup(
		t,
		"zero",
		mustRankingExample(t, "a", 0, 0, 1, 2),
		mustRankingExample(t, "b", 0, 2, 1, 0),
	)
	zeroModel, zeroReport, err := TrainLinearLambdaRank(
		t.Context(),
		definitions,
		[]QueryGroup{zeroGroup},
		DefaultLinearLambdaRankTrainingOptions(),
	)
	if err != nil {
		t.Fatalf("TrainLinearLambdaRank zero: %v", err)
	}
	if zeroReport.Iterations != 1 || zeroReport.PreferencePairs != 0 ||
		!reflect.DeepEqual(zeroModel.Weights(), []float64{0, 0, 0}) {
		t.Fatalf("zero training result = %v, %#v", zeroModel.Weights(), zeroReport)
	}
}

func TestLinearLambdaRankTrainingValidation(t *testing.T) {
	definitions := definitionsForTest("feature")
	group := mustQueryGroup(
		t,
		"query",
		mustRankingExample(t, "a", 1, 1),
		mustRankingExample(t, "b", 0, 0),
	)
	validOptions := DefaultLinearLambdaRankTrainingOptions()
	invalidOptions := validOptions
	invalidOptions.LearningRate = 0

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	cases := []struct {
		ctx         context.Context
		definitions []FeatureDefinition
		groups      []QueryGroup
		options     LinearLambdaRankTrainingOptions
	}{
		{nil, definitions, []QueryGroup{group}, validOptions},
		{t.Context(), nil, []QueryGroup{group}, validOptions},
		{t.Context(), definitions, nil, validOptions},
		{t.Context(), definitions, make([]QueryGroup, maximumLinearQueries+1), validOptions},
		{t.Context(), definitions, []QueryGroup{group}, invalidOptions},
		{t.Context(), definitions, []QueryGroup{{}}, validOptions},
		{t.Context(), definitions, []QueryGroup{mustQueryGroup(
			t,
			"wrong",
			mustRankingExample(t, "a", 0, 1, 2),
		)}, validOptions},
		{cancelled, definitions, []QueryGroup{group}, validOptions},
	}
	for _, testCase := range cases {
		_, _, err := TrainLinearLambdaRank(
			testCase.ctx,
			testCase.definitions,
			testCase.groups,
			testCase.options,
		)
		if err == nil {
			t.Errorf("TrainLinearLambdaRank accepted invalid input")
		}
	}
	_, _, err := TrainLinearLambdaRank(cancelled, definitions, []QueryGroup{group}, validOptions)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancellation error = %v", err)
	}
	for _, cancelAt := range []int{4, 5, 6} {
		ctx := &histogramCancellationContext{cancelAt: cancelAt}
		if _, _, err := TrainLinearLambdaRank(
			ctx,
			definitions,
			[]QueryGroup{group},
			validOptions,
		); !errors.Is(err, context.Canceled) {
			t.Errorf("staged cancellation %d = %v", cancelAt, err)
		}
	}
	if _, err := normalizeTrainingGroups(t.Context(), []QueryGroup{{}}, 1); err == nil {
		t.Fatal("invalid normalization group was accepted")
	}
}

func TestTrainingWorkBoundsAndInnerCancellation(t *testing.T) {
	pairGroup := boundedTrainingGroup(t, 2048, 1, true)
	if err := validateTrainingWork(t.Context(), []QueryGroup{pairGroup}, 1); err == nil {
		t.Fatal("preference pair overflow was accepted")
	}

	exampleGroup := boundedTrainingGroup(t, 11, 1, false)
	exampleGroups := make(
		[]QueryGroup,
		maximumTrainingExamples/len(exampleGroup.examples)+1,
	)
	for index := range exampleGroups {
		exampleGroups[index] = exampleGroup
	}
	if err := validateTrainingWork(t.Context(), exampleGroups, 1); err == nil {
		t.Fatal("training example overflow was accepted")
	}

	featureGroup := boundedTrainingGroup(t, 1000, 81, false)
	featureGroups := make([]QueryGroup, maximumTrainingExamples/len(featureGroup.examples))
	for index := range featureGroups {
		featureGroups[index] = featureGroup
	}
	if err := validateTrainingWork(t.Context(), featureGroups, 81); err == nil {
		t.Fatal("training feature value overflow was accepted")
	}

	innerCancellation := &histogramCancellationContext{cancelAt: 3}
	err := validateTrainingWork(innerCancellation, []QueryGroup{pairGroup}, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("inner validation cancellation = %v", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = normalizeTrainingGroups(cancelled, []QueryGroup{pairGroup}, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("normalization cancellation = %v", err)
	}
}

func boundedTrainingGroup(
	t *testing.T,
	exampleCount int,
	dimension int,
	preferences bool,
) QueryGroup {
	t.Helper()
	vector, err := NewFeatureVector(make([]float64, dimension))
	if err != nil {
		t.Fatal(err)
	}
	examples := make([]RankingExample, exampleCount)
	for index := range examples {
		relevance := 0
		if preferences && index >= exampleCount/2 {
			relevance = 1
		}
		examples[index], err = NewRankingExample(strconv.Itoa(index), relevance, vector)
		if err != nil {
			t.Fatal(err)
		}
	}
	group, err := NewQueryGroup("bounded", examples)
	if err != nil {
		t.Fatal(err)
	}

	return group
}

func TestLinearLambdaRankTrainingOptionValidation(t *testing.T) {
	valid := DefaultLinearLambdaRankTrainingOptions()
	if err := valid.validate(); err != nil {
		t.Fatalf("default options: %v", err)
	}

	cases := []LinearLambdaRankTrainingOptions{
		withTrainingOption(valid, func(options *LinearLambdaRankTrainingOptions) {
			options.LearningRate = 0
		}),
		withTrainingOption(valid, func(options *LinearLambdaRankTrainingOptions) {
			options.LearningRate = math.NaN()
		}),
		withTrainingOption(valid, func(options *LinearLambdaRankTrainingOptions) {
			options.Regularization = -1
		}),
		withTrainingOption(valid, func(options *LinearLambdaRankTrainingOptions) {
			options.MaximumIterations = maximumLinearTrainingIterations + 1
		}),
		withTrainingOption(valid, func(options *LinearLambdaRankTrainingOptions) {
			options.NormalizedDiscountedCumulativeGainCutoff = 0
		}),
		withTrainingOption(valid, func(options *LinearLambdaRankTrainingOptions) {
			options.MinimumWeightChange = math.Inf(1)
		}),
		withTrainingOption(valid, func(options *LinearLambdaRankTrainingOptions) {
			options.MaximumAbsoluteWeight = 0
		}),
	}
	for _, options := range cases {
		if err := options.validate(); err == nil {
			t.Errorf("options.validate accepted %#v", options)
		}
	}
}

func TestLambdaGradientMechanics(t *testing.T) {
	active := normalizedQueryGroup{
		queryIdentifier: "active",
		examples: []normalizedRankingExample{
			{documentIdentifier: "low", relevance: 0, values: []float64{0}},
			{documentIdentifier: "equal-one", relevance: 1, values: []float64{1}},
			{documentIdentifier: "equal-two", relevance: 1, values: []float64{2}},
			{documentIdentifier: "high", relevance: 3, values: []float64{3}},
		},
	}
	inactive := normalizedQueryGroup{
		queryIdentifier: "inactive",
		examples: []normalizedRankingExample{
			{documentIdentifier: "a", relevance: 0, values: []float64{0}},
			{documentIdentifier: "b", relevance: 0, values: []float64{1}},
		},
	}
	gradient, pairs, err := lambdaGradient(
		t.Context(),
		[]normalizedQueryGroup{active, inactive},
		[]float64{0},
		2,
	)
	if err != nil || pairs == 0 || gradient[0] <= 0 {
		t.Fatalf("gradient, pairs = %v, %d, %v", gradient, pairs, err)
	}
	large := normalizedQueryGroup{
		queryIdentifier: "cancel",
		examples:        make([]normalizedRankingExample, 128),
	}
	for index := range large.examples {
		large.examples[index] = normalizedRankingExample{
			documentIdentifier: strconv.Itoa(index),
			relevance:          index % 2,
			values:             []float64{float64(index)},
		}
	}
	if _, _, err := lambdaGradient(
		&histogramCancellationContext{cancelAt: 130},
		[]normalizedQueryGroup{large},
		[]float64{0},
		10,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("inner lambda cancellation = %v", err)
	}
	if _, _, err := queryLambdaGradient(
		&histogramCancellationContext{cancelAt: len(active.examples) + 1},
		active,
		[]float64{0},
		2,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("outer lambda cancellation = %v", err)
	}

	positions := scorePositions(active.examples, []float64{1, 3, 3, 0})
	if !reflect.DeepEqual(positions, []int{2, 0, 1, 3}) {
		t.Fatalf("score positions = %v", positions)
	}
	if preferred, other, distinct := preferredPair(active.examples, 0, 3); !distinct ||
		preferred != 3 || other != 0 {
		t.Fatalf("preferred pair = %d, %d, %v", preferred, other, distinct)
	}
	if preferred, other, distinct := preferredPair(active.examples, 3, 0); !distinct ||
		preferred != 3 || other != 0 {
		t.Fatalf("reverse preferred pair = %d, %d, %v", preferred, other, distinct)
	}
	if _, _, distinct := preferredPair(active.examples, 1, 2); distinct {
		t.Fatalf("equal grades produced a preference")
	}
	if idealDiscountedCumulativeGain(active.examples, 2) <= 0 || relevanceGain(3) != 7 {
		t.Fatalf("discounted gain calculation is invalid")
	}
	if rankDiscount(2, 2) != 0 || rankDiscount(0, 2) != 1 {
		t.Fatalf("rank discount is invalid")
	}
	if preferredPairProbability(2) >= 0.5 || preferredPairProbability(-2) <= 0.5 {
		t.Fatalf("pair probability is invalid")
	}
}

func TestLinearWeightUpdateConstraints(t *testing.T) {
	weights := []float64{0, 0, 0, 0}
	gradient := []float64{-100, 100, -100, 100}
	definitions := []FeatureDefinition{
		{Name: "up", Direction: FeatureIncreasing},
		{Name: "down", Direction: FeatureDecreasing},
		{Name: "free-negative", Direction: FeatureUnconstrained},
		{Name: "free-positive", Direction: FeatureUnconstrained},
	}
	options := DefaultLinearLambdaRankTrainingOptions()
	options.MaximumAbsoluteWeight = 0.5
	change := updateLinearWeights(weights, gradient, definitions, options)
	if !reflect.DeepEqual(weights, []float64{0, 0, -0.5, 0.5}) || change != 0.5 {
		t.Fatalf("constrained weights, change = %v, %v", weights, change)
	}
}

func withTrainingOption(
	options LinearLambdaRankTrainingOptions,
	change func(*LinearLambdaRankTrainingOptions),
) LinearLambdaRankTrainingOptions {
	change(&options)

	return options
}
