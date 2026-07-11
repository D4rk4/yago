package rankfit

import (
	"fmt"
	"math"
)

type histogramLeafRange struct {
	minimum float64
	maximum float64
}

func (m HistogramLambdaMARTModel) Validate() error {
	if err := validateFeatureDefinitions(m.featureDefinitions); err != nil {
		return err
	}
	if !finiteHistogramValue(m.learningRate, 1, false) {
		return fmt.Errorf("model learning rate must be finite and in (0, 1]")
	}
	if len(m.trees) > maximumHistogramTrees {
		return fmt.Errorf("model trees must not exceed %d", maximumHistogramTrees)
	}
	for index, tree := range m.trees {
		if err := validateHistogramRankingTree(tree, m.featureDefinitions); err != nil {
			return fmt.Errorf("tree %d: %w", index+1, err)
		}
	}

	return nil
}

func validateHistogramRankingTree(
	tree histogramRankingTree,
	featureDefinitions []FeatureDefinition,
) error {
	if tree.interactionGroup == "" || !validFeatureName(tree.interactionGroup) {
		return fmt.Errorf("interaction group is invalid")
	}
	allowed, err := histogramTreeAllowedFeatures(
		tree.allowedFeatureIndices,
		len(featureDefinitions),
	)
	if err != nil {
		return err
	}
	if tree.root == nil {
		return fmt.Errorf("root must not be nil")
	}
	_, err = validateHistogramTreeNode(tree.root, featureDefinitions, allowed, 0)

	return err
}

func histogramTreeAllowedFeatures(indices []int, dimension int) (map[int]struct{}, error) {
	if len(indices) == 0 || len(indices) > dimension {
		return nil, fmt.Errorf("allowed feature indices have an invalid size")
	}
	allowed := make(map[int]struct{}, len(indices))
	previous := 0
	havePrevious := false
	for _, feature := range indices {
		if feature < 0 || feature >= dimension {
			return nil, fmt.Errorf("allowed feature index is invalid")
		}
		if havePrevious && previous >= feature {
			return nil, fmt.Errorf("allowed feature indices must be unique and ordered")
		}
		allowed[feature] = struct{}{}
		previous = feature
		havePrevious = true
	}

	return allowed, nil
}

func validateHistogramTreeNode(
	node *histogramTreeNode,
	featureDefinitions []FeatureDefinition,
	allowed map[int]struct{},
	depth int,
) (histogramLeafRange, error) {
	if node == nil {
		return histogramLeafRange{}, fmt.Errorf("node must not be nil")
	}
	if depth > maximumHistogramDepth {
		return histogramLeafRange{}, fmt.Errorf(
			"tree depth must not exceed %d",
			maximumHistogramDepth,
		)
	}
	if node.leaf {
		return validateHistogramLeaf(node)
	}
	if depth == maximumHistogramDepth {
		return histogramLeafRange{}, fmt.Errorf(
			"tree depth must not exceed %d",
			maximumHistogramDepth,
		)
	}
	if node.value != 0 || node.left == nil || node.right == nil {
		return histogramLeafRange{}, fmt.Errorf("split node is malformed")
	}
	if _, exists := allowed[node.featureIndex]; !exists {
		return histogramLeafRange{}, fmt.Errorf(
			"split feature is not allowed by the interaction group",
		)
	}
	if math.IsNaN(node.threshold) || math.IsInf(node.threshold, 0) ||
		math.Abs(node.threshold) > maximumNormalizedFeatureMagnitude {
		return histogramLeafRange{}, fmt.Errorf("split threshold must be finite and bounded")
	}

	return validateHistogramSplitChildren(node, featureDefinitions, allowed, depth)
}

func validateHistogramLeaf(node *histogramTreeNode) (histogramLeafRange, error) {
	if node.left != nil || node.right != nil {
		return histogramLeafRange{}, fmt.Errorf("leaf must not have children")
	}
	if math.IsNaN(node.value) || math.IsInf(node.value, 0) ||
		math.Abs(node.value) > maximumHistogramLeafMagnitude {
		return histogramLeafRange{}, fmt.Errorf("leaf value must be finite and bounded")
	}

	return histogramLeafRange{minimum: node.value, maximum: node.value}, nil
}

func validateHistogramSplitChildren(
	node *histogramTreeNode,
	featureDefinitions []FeatureDefinition,
	allowed map[int]struct{},
	depth int,
) (histogramLeafRange, error) {
	left, err := validateHistogramTreeNode(node.left, featureDefinitions, allowed, depth+1)
	if err != nil {
		return histogramLeafRange{}, err
	}
	right, err := validateHistogramTreeNode(node.right, featureDefinitions, allowed, depth+1)
	if err != nil {
		return histogramLeafRange{}, err
	}
	direction := featureDefinitions[node.featureIndex].Direction
	if direction == FeatureIncreasing && left.maximum > right.minimum {
		return histogramLeafRange{}, fmt.Errorf("increasing feature violates monotonicity")
	}
	if direction == FeatureDecreasing && left.minimum < right.maximum {
		return histogramLeafRange{}, fmt.Errorf("decreasing feature violates monotonicity")
	}

	return histogramLeafRange{
		minimum: min(left.minimum, right.minimum),
		maximum: max(left.maximum, right.maximum),
	}, nil
}

func cloneHistogramRankingTrees(trees []histogramRankingTree) []histogramRankingTree {
	cloned := make([]histogramRankingTree, len(trees))
	for index, tree := range trees {
		cloned[index] = histogramRankingTree{
			interactionGroup:      tree.interactionGroup,
			allowedFeatureIndices: append([]int(nil), tree.allowedFeatureIndices...),
			root:                  cloneHistogramTreeNode(tree.root),
		}
	}

	return cloned
}

func cloneHistogramTreeNode(node *histogramTreeNode) *histogramTreeNode {
	if node == nil {
		return nil
	}

	return &histogramTreeNode{
		leaf:         node.leaf,
		value:        node.value,
		featureIndex: node.featureIndex,
		threshold:    node.threshold,
		left:         cloneHistogramTreeNode(node.left),
		right:        cloneHistogramTreeNode(node.right),
	}
}
