package rankfit

import (
	"context"
	"testing"
	"time"
)

func histogramLeaf(value float64) *histogramTreeNode {
	return &histogramTreeNode{leaf: true, value: value}
}

func histogramSplit(
	feature int,
	threshold float64,
	left *histogramTreeNode,
	right *histogramTreeNode,
) *histogramTreeNode {
	return &histogramTreeNode{
		featureIndex: feature,
		threshold:    threshold,
		left:         left,
		right:        right,
	}
}

func mustHistogramModel(
	t *testing.T,
	definitions []FeatureDefinition,
	learningRate float64,
	trees ...histogramRankingTree,
) HistogramLambdaMARTModel {
	t.Helper()
	model, err := newHistogramLambdaMARTModel(definitions, learningRate, trees)
	if err != nil {
		t.Fatalf("newHistogramLambdaMARTModel: %v", err)
	}

	return model
}

func histogramTree(
	group string,
	features []int,
	root *histogramTreeNode,
) histogramRankingTree {
	return histogramRankingTree{
		interactionGroup:      group,
		allowedFeatureIndices: features,
		root:                  root,
	}
}

type histogramCancellationContext struct {
	cancelAt int
	calls    int
}

func (c *histogramCancellationContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (c *histogramCancellationContext) Done() <-chan struct{} {
	return nil
}

func (c *histogramCancellationContext) Err() error {
	c.calls++
	if c.calls >= c.cancelAt {
		return context.Canceled
	}

	return nil
}

func (c *histogramCancellationContext) Value(any) any {
	return nil
}
