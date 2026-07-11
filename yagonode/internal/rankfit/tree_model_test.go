package rankfit

import (
	"math"
	"reflect"
	"testing"
)

func TestHistogramModelPredictionExplanationAndImmutability(t *testing.T) {
	definitions := []FeatureDefinition{
		{Name: "quality", Direction: FeatureIncreasing},
		{Name: "risk", Direction: FeatureDecreasing},
	}
	trees := []histogramRankingTree{
		histogramTree(
			"quality",
			[]int{0},
			histogramSplit(0, 0, histogramLeaf(-1), histogramLeaf(1)),
		),
		histogramTree("risk", []int{1}, histogramSplit(1, 0, histogramLeaf(1), histogramLeaf(-1))),
	}
	model := mustHistogramModel(t, definitions, 0.5, trees...)
	definitions[0].Name = "changed"
	trees[0].allowedFeatureIndices[0] = 1
	trees[0].root.left.value = 99
	returnedDefinitions := model.FeatureDefinitions()
	returnedDefinitions[0].Name = "also-changed"
	if model.FeatureDefinitions()[0].Name != "quality" || model.trees[0].root.left.value != -1 {
		t.Fatalf("model changed through caller data")
	}
	if model.LearningRate() != 0.5 || model.TreeCount() != 2 {
		t.Fatalf("model metadata = %v, %d", model.LearningRate(), model.TreeCount())
	}

	group := mustQueryGroup(
		t,
		"query",
		mustRankingExample(t, "best", 3, 2, 0),
		mustRankingExample(t, "middle", 1, 1, 1),
		mustRankingExample(t, "worst", 0, 0, 2),
	)
	predictions, err := model.Predict(group)
	if err != nil {
		t.Fatalf("Predict: %v", err)
	}
	assertHistogramPredictions(t, predictions)
	reversed := mustQueryGroup(
		t,
		"reversed",
		mustRankingExample(t, "worst", 0, 0, 2),
		mustRankingExample(t, "best", 3, 2, 0),
		mustRankingExample(t, "middle", 1, 1, 1),
	)
	if reordered, err := model.Predict(reversed); err != nil ||
		reordered[0].DocumentIdentifier != "best" {
		t.Fatalf("reversed prediction = %#v, %v", reordered, err)
	}

	explanations, err := model.Explain(group)
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	assertHistogramExplanations(t, explanations)
}

func TestHistogramModelKeepsMissingSplitEvidenceNeutral(t *testing.T) {
	model := mustHistogramModel(
		t,
		definitionsForTest("quality"),
		1,
		histogramTree(
			"quality",
			[]int{0},
			histogramSplit(0, 0, histogramLeaf(-1), histogramLeaf(1)),
		),
	)
	group := mustQueryGroup(
		t,
		"missing",
		mustKnownRankingExample(t, "missing", []float64{0}, []bool{false}),
		mustKnownRankingExample(t, "low", []float64{-1}, []bool{true}),
		mustKnownRankingExample(t, "high", []float64{1}, []bool{true}),
	)
	predictions, err := model.Predict(group)
	if err != nil {
		t.Fatalf("Predict: %v", err)
	}
	if got := []string{
		predictions[0].DocumentIdentifier,
		predictions[1].DocumentIdentifier,
		predictions[2].DocumentIdentifier,
	}; !reflect.DeepEqual(got, []string{"high", "missing", "low"}) ||
		predictions[1].Score != 0 {
		t.Fatalf("missing prediction = %#v", predictions)
	}
	explanations, err := model.Explain(group)
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	for _, explanation := range explanations {
		if explanation.DocumentIdentifier != "missing" {
			continue
		}
		decision := explanation.TreeContributions[0].Decisions[0]
		if explanation.Score != 0 || decision.Known || !decision.TerminatedMissing ||
			decision.FeatureName != "quality" {
			t.Fatalf("missing explanation = %#v", explanation)
		}

		return
	}
	t.Fatal("missing explanation was not returned")
}

func assertHistogramPredictions(t *testing.T, predictions []RankedDocument) {
	t.Helper()
	if got := []string{
		predictions[0].DocumentIdentifier,
		predictions[1].DocumentIdentifier,
		predictions[2].DocumentIdentifier,
	}; !reflect.DeepEqual(got, []string{"best", "middle", "worst"}) {
		t.Fatalf("prediction order = %v", got)
	}
	if predictions[0].Score != 1 || predictions[1].Score != 0 || predictions[2].Score != -1 {
		t.Fatalf("prediction scores = %#v", predictions)
	}
}

func assertHistogramExplanations(t *testing.T, explanations []HistogramRankingExplanation) {
	t.Helper()
	for index, explanation := range explanations {
		total := 0.0
		for treeIndex, contribution := range explanation.TreeContributions {
			total += contribution.Contribution
			if contribution.TreeIndex != treeIndex+1 || len(contribution.Decisions) != 1 ||
				contribution.InteractionGroup == "" || contribution.Decisions[0].FeatureName == "" {
				t.Fatalf("tree contribution = %#v", contribution)
			}
		}
		if explanation.Rank != index+1 || total != explanation.Score {
			t.Fatalf("explanation = %#v, total = %v", explanation, total)
		}
	}
}

func TestHistogramModelDeterministicTiesAndValidationFailures(t *testing.T) {
	model := mustHistogramModel(t, definitionsForTest("feature"), 0.1)
	group := mustQueryGroup(
		t,
		"ties",
		mustRankingExample(t, "b", 0, 1),
		mustRankingExample(t, "a", 0, 2),
	)
	predictions, err := model.Predict(group)
	if err != nil || predictions[0].DocumentIdentifier != "b" {
		t.Fatalf("tie prediction = %#v, err = %v", predictions, err)
	}
	explanations, err := model.Explain(group)
	if err != nil || explanations[0].DocumentIdentifier != "b" ||
		len(explanations[0].TreeContributions) != 0 {
		t.Fatalf("tie explanation = %#v, err = %v", explanations, err)
	}
	if _, err := model.Predict(QueryGroup{}); err == nil {
		t.Errorf("Predict accepted an invalid group")
	}
	if _, err := model.Explain(QueryGroup{}); err == nil {
		t.Errorf("Explain accepted an invalid group")
	}
	wrongDimension := mustQueryGroup(t, "wrong", mustRankingExample(t, "document", 0, 1, 2))
	if _, err := model.Predict(wrongDimension); err == nil {
		t.Errorf("Predict accepted a dimension mismatch")
	}
	if _, err := (HistogramLambdaMARTModel{}).Predict(group); err == nil {
		t.Errorf("Predict accepted an invalid model")
	}
	if _, err := newHistogramLambdaMARTModel(nil, 0.1, nil); err == nil {
		t.Errorf("newHistogramLambdaMARTModel accepted invalid definitions")
	}
}

func TestHistogramModelValidationRejectsCorruption(t *testing.T) {
	if _, err := newHistogramLambdaMARTModelWithPolicy(
		definitionsForTest("feature"),
		0.1,
		nil,
		0,
	); err == nil {
		t.Fatal("invalid missing policy was accepted")
	}
	for name, model := range invalidHistogramModels() {
		t.Run(name, func(t *testing.T) {
			if err := model.Validate(); err == nil {
				t.Fatalf("Validate accepted %#v", model)
			}
		})
	}

	definitions := []FeatureDefinition{
		{Name: "free"},
		{Name: "down", Direction: FeatureDecreasing},
	}
	valid := mustHistogramModel(
		t,
		definitions,
		0.1,
		histogramTree(
			"both",
			[]int{0, 1},
			histogramSplit(0, 0, histogramLeaf(-2), histogramLeaf(3)),
		),
	)
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate valid model: %v", err)
	}
	allowed, err := histogramTreeAllowedFeatures([]int{0, 1}, 2)
	if err != nil || len(allowed) != 2 {
		t.Fatalf("allowed features = %v, %v", allowed, err)
	}
	if _, err := validateHistogramTreeNode(nil, definitions, allowed, 0); err == nil {
		t.Errorf("nil node was accepted")
	}
	if _, err := validateHistogramTreeNode(histogramLeaf(0), definitions, allowed, 5); err == nil {
		t.Errorf("excessive depth was accepted")
	}
	if cloneHistogramTreeNode(nil) != nil {
		t.Errorf("nil node clone is not nil")
	}
}

func invalidHistogramModels() map[string]HistogramLambdaMARTModel {
	definitions := []FeatureDefinition{
		{Name: "up", Direction: FeatureIncreasing},
		{Name: "down", Direction: FeatureDecreasing},
	}
	models := invalidHistogramHeaderModels(definitions)
	for _, additions := range []map[string]HistogramLambdaMARTModel{
		invalidHistogramMetadataModels(definitions),
		invalidHistogramLeafModels(definitions),
		invalidHistogramSplitModels(definitions),
		invalidHistogramConstraintModels(definitions),
	} {
		for name, model := range additions {
			models[name] = model
		}
	}

	return models
}

func invalidHistogramHeaderModels(
	definitions []FeatureDefinition,
) map[string]HistogramLambdaMARTModel {
	validTree := func() histogramRankingTree {
		return histogramTree(
			"up",
			[]int{0},
			histogramSplit(0, 0, histogramLeaf(-1), histogramLeaf(1)),
		)
	}
	tooManyTrees := make([]histogramRankingTree, maximumHistogramTrees+1)
	for index := range tooManyTrees {
		tooManyTrees[index] = validTree()
	}

	return map[string]HistogramLambdaMARTModel{
		"definitions": {learningRate: 0.1, missingPolicy: missingEvidenceNeutral},
		"learning rate": {
			featureDefinitions: definitions,
			missingPolicy:      missingEvidenceNeutral,
		},
		"tree limit": {
			featureDefinitions: definitions,
			learningRate:       0.1,
			trees:              tooManyTrees,
			missingPolicy:      missingEvidenceNeutral,
		},
	}
}

func invalidHistogramMetadataModels(
	definitions []FeatureDefinition,
) map[string]HistogramLambdaMARTModel {
	return map[string]HistogramLambdaMARTModel{
		"group name": histogramValidationModel(
			definitions,
			histogramTree("bad name", []int{0}, histogramLeaf(0)),
		),
		"empty whitelist": histogramValidationModel(
			definitions,
			histogramTree("group", nil, histogramLeaf(0)),
		),
		"large whitelist": histogramValidationModel(
			definitions,
			histogramTree("group", []int{0, 1, 2}, histogramLeaf(0)),
		),
		"negative feature": histogramValidationModel(
			definitions,
			histogramTree("group", []int{-1}, histogramLeaf(0)),
		),
		"outside feature": histogramValidationModel(
			definitions,
			histogramTree("group", []int{2}, histogramLeaf(0)),
		),
		"unordered features": histogramValidationModel(
			definitions,
			histogramTree("group", []int{1, 0}, histogramLeaf(0)),
		),
		"nil root": histogramValidationModel(
			definitions,
			histogramTree("group", []int{0}, nil),
		),
	}
}

func invalidHistogramLeafModels(
	definitions []FeatureDefinition,
) map[string]HistogramLambdaMARTModel {
	return map[string]HistogramLambdaMARTModel{
		"leaf children": histogramValidationModel(definitions, histogramTree(
			"group", []int{0}, &histogramTreeNode{leaf: true, left: histogramLeaf(0)},
		)),
		"leaf NaN": histogramValidationModel(
			definitions,
			histogramTree("group", []int{0}, histogramLeaf(math.NaN())),
		),
		"leaf infinite": histogramValidationModel(
			definitions,
			histogramTree("group", []int{0}, histogramLeaf(math.Inf(1))),
		),
		"leaf bound": histogramValidationModel(
			definitions,
			histogramTree("group", []int{0}, histogramLeaf(maximumHistogramLeafMagnitude+1)),
		),
	}
}

func invalidHistogramSplitModels(
	definitions []FeatureDefinition,
) map[string]HistogramLambdaMARTModel {
	return map[string]HistogramLambdaMARTModel{
		"split value": histogramValidationModel(definitions, histogramTree(
			"group",
			[]int{0},
			&histogramTreeNode{value: 1, left: histogramLeaf(0), right: histogramLeaf(1)},
		)),
		"split child": histogramValidationModel(
			definitions,
			histogramTree("group", []int{0}, &histogramTreeNode{}),
		),
		"split feature": histogramValidationModel(
			definitions,
			histogramTree(
				"group",
				[]int{0},
				histogramSplit(1, 0, histogramLeaf(1), histogramLeaf(0)),
			),
		),
		"threshold NaN": histogramValidationModel(
			definitions,
			histogramTree(
				"group",
				[]int{0},
				histogramSplit(0, math.NaN(), histogramLeaf(-1), histogramLeaf(1)),
			),
		),
		"threshold infinite": histogramValidationModel(
			definitions,
			histogramTree(
				"group",
				[]int{0},
				histogramSplit(0, math.Inf(1), histogramLeaf(-1), histogramLeaf(1)),
			),
		),
		"threshold bound": histogramValidationModel(
			definitions,
			histogramTree(
				"group",
				[]int{0},
				histogramSplit(0, 9, histogramLeaf(-1), histogramLeaf(1)),
			),
		),
	}
}

func invalidHistogramConstraintModels(
	definitions []FeatureDefinition,
) map[string]HistogramLambdaMARTModel {
	deep := histogramLeaf(1)
	for range maximumHistogramDepth + 1 {
		deep = histogramSplit(0, 0, histogramLeaf(-1), deep)
	}
	leftInvalid := histogramSplit(0, 0, histogramLeaf(math.NaN()), histogramLeaf(1))
	rightInvalid := histogramSplit(0, 0, histogramLeaf(-1), histogramLeaf(math.NaN()))

	return map[string]HistogramLambdaMARTModel{
		"increasing": histogramValidationModel(
			definitions,
			histogramTree(
				"group",
				[]int{0},
				histogramSplit(0, 0, histogramLeaf(1), histogramLeaf(-1)),
			),
		),
		"decreasing": histogramValidationModel(
			definitions,
			histogramTree(
				"group",
				[]int{1},
				histogramSplit(1, 0, histogramLeaf(-1), histogramLeaf(1)),
			),
		),
		"left child": histogramValidationModel(
			definitions,
			histogramTree("group", []int{0}, leftInvalid),
		),
		"right child": histogramValidationModel(
			definitions,
			histogramTree("group", []int{0}, rightInvalid),
		),
		"depth": histogramValidationModel(
			definitions,
			histogramTree("group", []int{0}, deep),
		),
	}
}

func histogramValidationModel(
	definitions []FeatureDefinition,
	tree histogramRankingTree,
) HistogramLambdaMARTModel {
	return HistogramLambdaMARTModel{
		featureDefinitions: definitions,
		learningRate:       0.1,
		trees:              []histogramRankingTree{tree},
		missingPolicy:      missingEvidenceNeutral,
	}
}
