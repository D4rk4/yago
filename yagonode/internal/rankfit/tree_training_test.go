package rankfit

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestHistogramLambdaMARTTrainingLearnsBoundedMonotoneRanking(t *testing.T) {
	definitions, group := histogramRankingFixture(t)
	options := histogramTrainingOptions()
	model, report, err := TrainHistogramLambdaMART(
		t.Context(),
		definitions,
		[]QueryGroup{group},
		options,
	)
	if err != nil {
		t.Fatalf("TrainHistogramLambdaMART: %v", err)
	}
	if report.Trees != options.MaximumTrees || report.PreferencePairs != 6 ||
		model.TreeCount() != options.MaximumTrees {
		t.Fatalf("report, model = %#v, %d", report, model.TreeCount())
	}
	if err := model.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, tree := range model.trees {
		allowed, err := histogramTreeAllowedFeatures(
			tree.allowedFeatureIndices,
			len(definitions),
		)
		if err != nil {
			t.Fatalf("allowed features: %v", err)
		}
		assertHistogramTreeFeatures(t, tree.root, allowed)
	}
	predictions, err := model.Predict(group)
	if err != nil {
		t.Fatalf("Predict: %v", err)
	}
	want := []string{"best", "good", "bad", "worst"}
	got := make([]string, len(predictions))
	for index, prediction := range predictions {
		got[index] = prediction.DocumentIdentifier
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("prediction order = %v, want %v", got, want)
	}
}

func TestHistogramLambdaMARTChoosesDeterministicInteractionGroup(t *testing.T) {
	definitions := definitionsForTest("first", "second")
	group := mustQueryGroup(
		t,
		"tie",
		mustRankingExample(t, "best", 2, 1, 1),
		mustRankingExample(t, "worst", 0, 0, 0),
	)
	options := histogramTrainingOptions()
	options.MaximumTrees = 1
	options.FeatureInteractionGroups = []FeatureInteractionGroup{
		{Name: "zeta", FeatureIndices: []int{1}},
		{Name: "alpha", FeatureIndices: []int{0}},
	}
	var expected []byte
	for iteration := range 20 {
		model, report, err := TrainHistogramLambdaMART(
			t.Context(), definitions, []QueryGroup{group}, options,
		)
		if err != nil || report.Trees != 1 || model.trees[0].interactionGroup != "alpha" {
			t.Fatalf("iteration %d result = %#v, %#v, %v", iteration, model, report, err)
		}
		encoded, err := json.Marshal(model)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if iteration == 0 {
			expected = encoded
		} else if !reflect.DeepEqual(encoded, expected) {
			t.Fatalf("iteration %d model differs: %s != %s", iteration, encoded, expected)
		}
	}
}

func TestHistogramLambdaMARTStopsWithoutLearningSignal(t *testing.T) {
	definitions := definitionsForTest("feature")
	options := histogramTrainingOptions()
	options.FeatureInteractionGroups = nil
	equal := mustQueryGroup(
		t,
		"equal",
		mustRankingExample(t, "b", 0, 1),
		mustRankingExample(t, "a", 0, 0),
	)
	model, report, err := TrainHistogramLambdaMART(
		t.Context(), definitions, []QueryGroup{equal}, options,
	)
	if err != nil || report.Trees != 0 || report.PreferencePairs != 0 || model.TreeCount() != 0 {
		t.Fatalf("equal result = %#v, %#v, %v", model, report, err)
	}
	constant := mustQueryGroup(
		t,
		"constant",
		mustRankingExample(t, "best", 2, 1),
		mustRankingExample(t, "worst", 0, 1),
	)
	model, report, err = TrainHistogramLambdaMART(
		t.Context(), definitions, []QueryGroup{constant}, options,
	)
	if err != nil || report.Trees != 0 || report.PreferencePairs != 1 || model.TreeCount() != 0 {
		t.Fatalf("constant result = %#v, %#v, %v", model, report, err)
	}
}

func TestHistogramLambdaMARTTrainingValidationAndCancellation(t *testing.T) {
	definitions, group := histogramRankingFixture(t)
	valid := histogramTrainingOptions()
	invalidOptions := valid
	invalidOptions.MaximumTrees = 0
	tooMany := make([]QueryGroup, maximumLinearQueries+1)
	wrongDimension := mustQueryGroup(t, "wrong", mustRankingExample(t, "document", 0, 1))
	cases := []struct {
		ctx         context.Context
		definitions []FeatureDefinition
		groups      []QueryGroup
		options     HistogramLambdaMARTTrainingOptions
	}{
		{nil, definitions, []QueryGroup{group}, valid},
		{t.Context(), nil, []QueryGroup{group}, valid},
		{t.Context(), definitions, nil, valid},
		{t.Context(), definitions, tooMany, valid},
		{t.Context(), definitions, []QueryGroup{group}, invalidOptions},
		{t.Context(), definitions, []QueryGroup{{}}, valid},
		{t.Context(), definitions, []QueryGroup{wrongDimension}, valid},
	}
	for _, testCase := range cases {
		if _, _, err := TrainHistogramLambdaMART(
			testCase.ctx,
			testCase.definitions,
			testCase.groups,
			testCase.options,
		); err == nil {
			t.Errorf("TrainHistogramLambdaMART accepted invalid input")
		}
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := TrainHistogramLambdaMART(
		cancelled, definitions, []QueryGroup{group}, valid,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
	for _, cancelAt := range []int{2, 3, 7, 8, 9, 13} {
		staged := &histogramCancellationContext{cancelAt: cancelAt}
		if _, _, err := TrainHistogramLambdaMART(
			staged, definitions, []QueryGroup{group}, valid,
		); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelAt %d error = %v", cancelAt, err)
		}
	}
}

func histogramRankingFixture(t *testing.T) ([]FeatureDefinition, QueryGroup) {
	t.Helper()
	definitions := []FeatureDefinition{
		{Name: "quality", Direction: FeatureIncreasing},
		{Name: "risk", Direction: FeatureDecreasing},
		{Name: "noise"},
	}
	group := mustQueryGroup(
		t,
		"ranking",
		mustRankingExample(t, "best", 3, 3, 0, 0),
		mustRankingExample(t, "good", 2, 2, 1, 1),
		mustRankingExample(t, "bad", 1, 1, 2, 0),
		mustRankingExample(t, "worst", 0, 0, 3, 1),
	)

	return definitions, group
}

func histogramTrainingOptions() HistogramLambdaMARTTrainingOptions {
	options := DefaultHistogramLambdaMARTTrainingOptions()
	options.MaximumTrees = 4
	options.MaximumDepth = 2
	options.MaximumBins = 4
	options.MinimumLeafExamples = 1
	options.FeatureInteractionGroups = []FeatureInteractionGroup{
		{Name: "quality", FeatureIndices: []int{0}},
		{Name: "risk", FeatureIndices: []int{1}},
		{Name: "noise", FeatureIndices: []int{2}},
	}

	return options
}

func assertHistogramTreeFeatures(
	t *testing.T,
	node *histogramTreeNode,
	allowed map[int]struct{},
) {
	t.Helper()
	if node.leaf {
		return
	}
	if _, exists := allowed[node.featureIndex]; !exists {
		t.Fatalf("feature %d is outside whitelist %v", node.featureIndex, allowed)
	}
	assertHistogramTreeFeatures(t, node.left, allowed)
	assertHistogramTreeFeatures(t, node.right, allowed)
}
