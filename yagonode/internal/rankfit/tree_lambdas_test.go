package rankfit

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strconv"
	"testing"
)

func TestHistogramLambdaDerivativesUseNDCGWeightsAndCurvature(t *testing.T) {
	active := normalizedQueryGroup{
		queryIdentifier: "active",
		examples: []normalizedRankingExample{
			{documentIdentifier: "a-top", relevance: 3, values: []float64{3}},
			{documentIdentifier: "b-top", relevance: 2, values: []float64{2}},
			{documentIdentifier: "c-low", relevance: 1, values: []float64{1}},
			{documentIdentifier: "d-low", relevance: 0, values: []float64{0}},
			{documentIdentifier: "e-low", relevance: 0, values: []float64{-1}},
		},
	}
	gradients, hessians, pairs, err := queryHistogramLambdaDerivatives(
		t.Context(),
		active,
		make([]float64, len(active.examples)),
		2,
	)
	if err != nil {
		t.Fatalf("queryHistogramLambdaDerivatives: %v", err)
	}
	if pairs != 7 || gradients[0] <= 0 || gradients[len(gradients)-1] >= 0 {
		t.Fatalf("gradients, pairs = %v, %d", gradients, pairs)
	}
	gradientTotal := 0.0
	for index, gradient := range gradients {
		gradientTotal += gradient
		if hessians[index] < 0 {
			t.Fatalf("negative hessian = %v", hessians)
		}
	}
	if math.Abs(gradientTotal) > 1e-12 {
		t.Fatalf("gradient total = %v", gradientTotal)
	}

	inactive := normalizedQueryGroup{
		queryIdentifier: "inactive",
		examples: []normalizedRankingExample{
			{documentIdentifier: "a", relevance: 0, values: []float64{0}},
			{documentIdentifier: "b", relevance: 0, values: []float64{1}},
		},
	}
	zeroGradient, zeroHessian, zeroPairs, err := queryHistogramLambdaDerivatives(
		t.Context(), inactive, []float64{0, 0}, 2,
	)
	if err != nil || zeroPairs != 0 ||
		!reflect.DeepEqual(zeroGradient, []float64{0, 0}) ||
		!reflect.DeepEqual(zeroHessian, []float64{0, 0}) {
		t.Fatalf("inactive derivatives = %v, %v, %d, %v", zeroGradient, zeroHessian, zeroPairs, err)
	}
}

func TestHistogramLambdaDerivativeAggregationAndCancellation(t *testing.T) {
	groups := []normalizedQueryGroup{
		{
			queryIdentifier: "active",
			examples: []normalizedRankingExample{
				{documentIdentifier: "best", relevance: 2, values: []float64{1}},
				{documentIdentifier: "worst", relevance: 0, values: []float64{-1}},
			},
		},
		{
			queryIdentifier: "inactive",
			examples: []normalizedRankingExample{
				{documentIdentifier: "only", relevance: 0, values: []float64{0}},
			},
		},
	}
	scores := newHistogramScoreMatrix(groups)
	if !reflect.DeepEqual([]int{len(scores[0]), len(scores[1])}, []int{2, 1}) {
		t.Fatalf("score matrix sizes = %v, %v", len(scores[0]), len(scores[1]))
	}
	derivatives, err := histogramLambdaDerivativesForGroups(t.Context(), groups, scores, 2)
	if err != nil || derivatives.preferencePairs != 1 || len(derivatives.gradients) != 3 ||
		derivatives.gradients[0] <= 0 || derivatives.gradients[1] >= 0 ||
		derivatives.gradients[2] != 0 {
		t.Fatalf("aggregated derivatives = %#v, %v", derivatives, err)
	}
	empty, err := histogramLambdaDerivativesForGroups(t.Context(), nil, nil, 1)
	if err != nil || len(empty.gradients) != 0 || len(empty.hessians) != 0 {
		t.Fatalf("empty derivatives = %#v, %v", empty, err)
	}

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, _, err := queryHistogramLambdaDerivatives(
		cancelled,
		groups[0],
		scores[0],
		2,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("query cancellation error = %v", err)
	}
	if _, err := histogramLambdaDerivativesForGroups(cancelled, groups, scores, 2); err == nil {
		t.Fatalf("aggregate cancellation succeeded")
	}
	staged := &histogramCancellationContext{cancelAt: 2}
	if _, err := histogramLambdaDerivativesForGroups(staged, groups, scores, 2); err == nil {
		t.Fatalf("staged aggregate cancellation succeeded")
	}
	large := normalizedQueryGroup{examples: make([]normalizedRankingExample, 128)}
	for index := range large.examples {
		large.examples[index] = normalizedRankingExample{
			documentIdentifier: strconv.Itoa(index),
			relevance:          index % 2,
			values:             []float64{float64(index)},
		}
	}
	if _, _, _, err := queryHistogramLambdaDerivatives(
		&histogramCancellationContext{cancelAt: 2},
		large,
		make([]float64, len(large.examples)),
		10,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("inner pair cancellation = %v", err)
	}
}
