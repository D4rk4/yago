package rankfit

import (
	"context"
	"fmt"
)

type histogramLambdaDerivatives struct {
	gradients       []float64
	hessians        []float64
	preferencePairs int
}

func newHistogramScoreMatrix(groups []normalizedQueryGroup) [][]float64 {
	scores := make([][]float64, len(groups))
	for index, group := range groups {
		scores[index] = make([]float64, len(group.examples))
	}

	return scores
}

func histogramLambdaDerivativesForGroups(
	ctx context.Context,
	groups []normalizedQueryGroup,
	scores [][]float64,
	cutoff int,
) (histogramLambdaDerivatives, error) {
	totalExamples := 0
	for _, group := range groups {
		totalExamples += len(group.examples)
	}
	derivatives := histogramLambdaDerivatives{
		gradients: make([]float64, totalExamples),
		hessians:  make([]float64, totalExamples),
	}
	offset := 0
	for index, group := range groups {
		gradient, hessian, pairs, err := queryHistogramLambdaDerivatives(
			ctx,
			group,
			scores[index],
			cutoff,
		)
		if err != nil {
			return histogramLambdaDerivatives{}, err
		}
		copy(derivatives.gradients[offset:], gradient)
		copy(derivatives.hessians[offset:], hessian)
		derivatives.preferencePairs += pairs
		offset += len(group.examples)
	}

	return derivatives, nil
}

func queryHistogramLambdaDerivatives(
	ctx context.Context,
	group normalizedQueryGroup,
	scores []float64,
	cutoff int,
) ([]float64, []float64, int, error) {
	gradients := make([]float64, len(group.examples))
	hessians := make([]float64, len(group.examples))
	ideal := idealDiscountedCumulativeGain(group.examples, cutoff)
	if ideal == 0 {
		return gradients, hessians, 0, nil
	}
	positions := scorePositions(group.examples, scores)
	pairs := 0
	for left := range group.examples {
		if err := ctx.Err(); err != nil {
			return nil, nil, 0, fmt.Errorf("compute histogram lambdas: %w", err)
		}
		for right := left + 1; right < len(group.examples); right++ {
			if right&63 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, nil, 0, fmt.Errorf("compute histogram lambdas: %w", err)
				}
			}
			preferred, other, distinct := preferredPair(group.examples, left, right)
			if !distinct {
				continue
			}
			delta := normalizedDiscountedCumulativeGainSwap(
				group.examples,
				preferred,
				other,
				discountedGainContext{positions: positions, cutoff: cutoff, ideal: ideal},
			)
			if delta == 0 {
				continue
			}
			probability := preferredPairProbability(scores[preferred] - scores[other])
			lambda := delta * probability
			curvature := delta * probability * (1 - probability)
			gradients[preferred] += lambda
			gradients[other] -= lambda
			hessians[preferred] += curvature
			hessians[other] += curvature
			pairs++
		}
	}

	return gradients, hessians, pairs, nil
}
