package rankfit

import "testing"

func TestHistogramLambdaMARTLearnsAllowedFeatureInteraction(t *testing.T) {
	definitions := definitionsForTest("first", "second")
	group := mustQueryGroup(
		t,
		"interaction",
		mustRankingExample(t, "base", 0, 0, 0),
		mustRankingExample(t, "conditional-best", 3, 1, 0),
		mustRankingExample(t, "other-good", 2, 0, 1),
		mustRankingExample(t, "conditional-lower", 1, 1, 1),
	)
	options := histogramTrainingOptions()
	options.MaximumTrees = 8
	options.FeatureInteractionGroups = []FeatureInteractionGroup{{
		Name: "pair", FeatureIndices: []int{0, 1},
	}}
	model, _, err := TrainHistogramLambdaMART(
		t.Context(), definitions, []QueryGroup{group}, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	interaction := false
	for _, tree := range model.trees {
		if histogramTreeUsesMultipleFeatures(tree.root, map[int]struct{}{}) {
			interaction = true
			break
		}
	}
	if !interaction {
		t.Fatalf("model did not learn a feature interaction: %#v", model)
	}
	predictions, err := model.Predict(group)
	if err != nil || predictions[0].DocumentIdentifier != "conditional-best" {
		t.Fatalf("interaction ranking = %#v, %v", predictions, err)
	}
}

func histogramTreeUsesMultipleFeatures(
	node *histogramTreeNode,
	path map[int]struct{},
) bool {
	if node.leaf {
		return len(path) > 1
	}
	next := make(map[int]struct{}, len(path)+1)
	for feature := range path {
		next[feature] = struct{}{}
	}
	next[node.featureIndex] = struct{}{}

	return histogramTreeUsesMultipleFeatures(node.left, next) ||
		histogramTreeUsesMultipleFeatures(node.right, next)
}
