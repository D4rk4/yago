package rankfit

import (
	"context"
	"testing"
)

func TestHistogramSplitRefusesLeavesBelowTheMinimumExampleBudget(t *testing.T) {
	builder := histogramLeafBudgetFixture(t.Context())
	rows := allHistogramRowIndices(len(builder.set.rows))
	total := builder.statistics(rows)
	bounds := histogramValueBounds{lower: -1, upper: 1}

	builder.options.MinimumLeafExamples = 1
	split, found, err := builder.bestSplit(rows, total, bounds)
	if err != nil || !found || split.threshold != -1.5 {
		t.Fatalf("unbudgeted split = %#v, %v, %v", split, found, err)
	}

	builder.options.MinimumLeafExamples = 2
	split, found, err = builder.bestSplit(rows, total, bounds)
	if err != nil || !found || split.threshold != 0 {
		t.Fatalf("budgeted split = %#v, %v, %v", split, found, err)
	}
	if _, accepted := builder.splitCandidate(
		0,
		-1.5,
		histogramNodeStatistics{count: 1, gradient: -1, hessian: 1},
		histogramNodeStatistics{count: 3, gradient: 1, hessian: 3},
		histogramSplitContext{bounds: bounds},
	); accepted {
		t.Fatal("single-example leaf was accepted")
	}

	builder.options.MinimumLeafExamples = 3
	if split, found, err = builder.bestSplit(rows, total, bounds); err != nil || found {
		t.Fatalf("split above the example budget = %#v, %v, %v", split, found, err)
	}
}

func histogramLeafBudgetFixture(ctx context.Context) histogramTreeBuilder {
	return histogramTreeBuilder{
		ctx: ctx,
		set: histogramTrainingSet{
			rows: []histogramTrainingRow{
				{values: []float64{-2}, known: []bool{true}},
				{values: []float64{-1}, known: []bool{true}},
				{values: []float64{1}, known: []bool{true}},
				{values: []float64{2}, known: []bool{true}},
			},
			thresholds: [][]float64{{-1.5, 0}},
		},
		derivatives: histogramLambdaDerivatives{
			gradients: []float64{-1, 0.5, 0.25, 0.25},
			hessians:  []float64{1, 1, 1, 1},
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
