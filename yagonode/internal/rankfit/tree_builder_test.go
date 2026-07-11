package rankfit

import (
	"context"
	"errors"
	"testing"
)

func TestHistogramTreeBuilderCancellationPathsAndZeroDenominator(t *testing.T) {
	for _, cancelAt := range []int{3, 4} {
		builder := histogramBuilderFixture(&histogramCancellationContext{cancelAt: cancelAt})
		if _, _, _, err := builder.build(); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelAt %d error = %v", cancelAt, err)
		}
	}
	builder := histogramBuilderFixture(&histogramCancellationContext{cancelAt: 1})
	rows := allHistogramRowIndices(len(builder.set.rows))
	statistics := builder.statistics(rows)
	if _, _, err := builder.bestSplit(
		rows,
		statistics,
		histogramValueBounds{lower: -1, upper: 1},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("bestSplit cancellation error = %v", err)
	}
	builder.options.L2Regularization = 0
	if got := builder.leafValue(
		histogramNodeStatistics{},
		histogramValueBounds{lower: -1, upper: 1},
	); got != 0 {
		t.Fatalf("zero denominator leaf = %v", got)
	}
}

func TestHistogramMonotoneBoundsRejectInvertedChildren(t *testing.T) {
	builder := histogramBuilderFixture(t.Context())
	bounds := histogramValueBounds{lower: -1, upper: 1}
	leftHigh := histogramNodeStatistics{count: 1, gradient: 1, hessian: 1}
	rightLow := histogramNodeStatistics{count: 1, gradient: -1, hessian: 1}
	builder.featureDefinitions[0].Direction = FeatureIncreasing
	if _, _, allowed := builder.constrainedChildBounds(0, leftHigh, rightLow, bounds); allowed {
		t.Fatal("inverted increasing children were accepted")
	}
	if _, accepted := builder.splitCandidate(
		0,
		0,
		leftHigh,
		rightLow,
		histogramSplitContext{bounds: bounds},
	); accepted {
		t.Fatal("inverted increasing split was accepted")
	}
	builder.featureDefinitions[0].Direction = FeatureDecreasing
	if _, _, allowed := builder.constrainedChildBounds(0, rightLow, leftHigh, bounds); allowed {
		t.Fatal("inverted decreasing children were accepted")
	}
}

func histogramBuilderFixture(ctx context.Context) histogramTreeBuilder {
	return histogramTreeBuilder{
		ctx: ctx,
		set: histogramTrainingSet{
			rows: []histogramTrainingRow{
				{values: []float64{-1}},
				{values: []float64{1}},
			},
			thresholds: [][]float64{{0}},
		},
		derivatives: histogramLambdaDerivatives{
			gradients: []float64{-0.5, 0.5},
			hessians:  []float64{0.25, 0.25},
		},
		featureDefinitions: definitionsForTest("feature"),
		interactionGroup: FeatureInteractionGroup{
			Name:           "feature",
			FeatureIndices: []int{0},
		},
		options: HistogramLambdaMARTTrainingOptions{
			MaximumDepth:        1,
			L2Regularization:    1,
			MinimumLeafExamples: 1,
		},
	}
}
