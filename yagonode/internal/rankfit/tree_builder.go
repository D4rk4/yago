package rankfit

import (
	"context"
	"fmt"
)

type histogramBucket struct {
	count    int
	gradient float64
	hessian  float64
}

type histogramNodeStatistics struct {
	count    int
	gradient float64
	hessian  float64
}

type histogramValueBounds struct {
	lower float64
	upper float64
}

type histogramSplitCandidate struct {
	featureIndex int
	threshold    float64
	gain         float64
	leftBounds   histogramValueBounds
	rightBounds  histogramValueBounds
}

type histogramSplitContext struct {
	bounds          histogramValueBounds
	parentObjective float64
}

type histogramTreeBuilder struct {
	ctx                context.Context
	set                histogramTrainingSet
	derivatives        histogramLambdaDerivatives
	featureDefinitions []FeatureDefinition
	interactionGroup   FeatureInteractionGroup
	options            HistogramLambdaMARTTrainingOptions
}

func (b histogramTreeBuilder) build() (histogramRankingTree, float64, bool, error) {
	root, gain, split, err := b.buildNode(
		allHistogramRowIndices(len(b.set.rows)),
		0,
		histogramValueBounds{
			lower: -maximumHistogramLeafMagnitude,
			upper: maximumHistogramLeafMagnitude,
		},
	)
	if err != nil {
		return histogramRankingTree{}, 0, false, err
	}

	return histogramRankingTree{
		interactionGroup:      b.interactionGroup.Name,
		allowedFeatureIndices: append([]int(nil), b.interactionGroup.FeatureIndices...),
		root:                  root,
	}, gain, split, nil
}

func (b histogramTreeBuilder) buildNode(
	rowIndices []int,
	depth int,
	bounds histogramValueBounds,
) (*histogramTreeNode, float64, bool, error) {
	if err := b.ctx.Err(); err != nil {
		return nil, 0, false, fmt.Errorf("build histogram tree: %w", err)
	}
	statistics := b.statistics(rowIndices)
	leaf := &histogramTreeNode{
		leaf:  true,
		value: b.leafValue(statistics, bounds),
	}
	if depth >= b.options.MaximumDepth ||
		len(rowIndices) < 2*b.options.MinimumLeafExamples {
		return leaf, 0, false, nil
	}
	split, found, err := b.bestSplit(rowIndices, statistics, bounds)
	if err != nil || !found {
		return leaf, 0, false, err
	}
	leftRows, rightRows := b.partitionRows(rowIndices, split)
	left, leftGain, _, err := b.buildNode(leftRows, depth+1, split.leftBounds)
	if err != nil {
		return nil, 0, false, err
	}
	right, rightGain, _, err := b.buildNode(rightRows, depth+1, split.rightBounds)
	if err != nil {
		return nil, 0, false, err
	}

	return &histogramTreeNode{
		featureIndex: split.featureIndex,
		threshold:    split.threshold,
		left:         left,
		right:        right,
	}, split.gain + leftGain + rightGain, true, nil
}

func (b histogramTreeBuilder) bestSplit(
	rowIndices []int,
	total histogramNodeStatistics,
	bounds histogramValueBounds,
) (histogramSplitCandidate, bool, error) {
	best := histogramSplitCandidate{}
	found := false
	parentValue := b.leafValue(total, bounds)
	parentObjective := b.objective(total, parentValue)
	for _, feature := range b.interactionGroup.FeatureIndices {
		if err := b.ctx.Err(); err != nil {
			return histogramSplitCandidate{}, false, fmt.Errorf("build histogram split: %w", err)
		}
		thresholds := b.set.thresholds[feature]
		buckets := b.buckets(rowIndices, feature, thresholds)
		left := histogramNodeStatistics{}
		for index, threshold := range thresholds {
			left = addHistogramStatistics(left, buckets[index])
			right := subtractHistogramStatistics(total, left)
			candidate, ok := b.splitCandidate(
				feature,
				threshold,
				left,
				right,
				histogramSplitContext{bounds: bounds, parentObjective: parentObjective},
			)
			if ok && (!found || candidate.gain > best.gain+minimumHistogramGain) {
				best = candidate
				found = true
			}
		}
	}

	return best, found, nil
}

func (b histogramTreeBuilder) splitCandidate(
	feature int,
	threshold float64,
	left histogramNodeStatistics,
	right histogramNodeStatistics,
	context histogramSplitContext,
) (histogramSplitCandidate, bool) {
	if left.count < b.options.MinimumLeafExamples ||
		right.count < b.options.MinimumLeafExamples {
		return histogramSplitCandidate{}, false
	}
	leftBounds, rightBounds, allowed := b.constrainedChildBounds(
		feature,
		left,
		right,
		context.bounds,
	)
	if !allowed {
		return histogramSplitCandidate{}, false
	}
	leftValue := b.leafValue(left, leftBounds)
	rightValue := b.leafValue(right, rightBounds)
	gain := b.objective(left, leftValue) + b.objective(right, rightValue) -
		context.parentObjective
	if gain <= minimumHistogramGain {
		return histogramSplitCandidate{}, false
	}

	return histogramSplitCandidate{
		featureIndex: feature,
		threshold:    threshold,
		gain:         gain,
		leftBounds:   leftBounds,
		rightBounds:  rightBounds,
	}, true
}

func (b histogramTreeBuilder) constrainedChildBounds(
	feature int,
	left histogramNodeStatistics,
	right histogramNodeStatistics,
	bounds histogramValueBounds,
) (histogramValueBounds, histogramValueBounds, bool) {
	leftBounds := bounds
	rightBounds := bounds
	leftValue := b.leafValue(left, bounds)
	rightValue := b.leafValue(right, bounds)
	switch b.featureDefinitions[feature].Direction {
	case FeatureIncreasing:
		if leftValue > rightValue {
			return histogramValueBounds{}, histogramValueBounds{}, false
		}
		middle := leftValue + (rightValue-leftValue)/2
		leftBounds.upper = min(leftBounds.upper, middle)
		rightBounds.lower = max(rightBounds.lower, middle)
	case FeatureDecreasing:
		if leftValue < rightValue {
			return histogramValueBounds{}, histogramValueBounds{}, false
		}
		middle := rightValue + (leftValue-rightValue)/2
		leftBounds.lower = max(leftBounds.lower, middle)
		rightBounds.upper = min(rightBounds.upper, middle)
	}

	return leftBounds, rightBounds, true
}

func (b histogramTreeBuilder) statistics(rowIndices []int) histogramNodeStatistics {
	statistics := histogramNodeStatistics{count: len(rowIndices)}
	for _, row := range rowIndices {
		statistics.gradient += b.derivatives.gradients[row]
		statistics.hessian += b.derivatives.hessians[row]
	}

	return statistics
}

func (b histogramTreeBuilder) buckets(
	rowIndices []int,
	feature int,
	thresholds []float64,
) []histogramBucket {
	buckets := make([]histogramBucket, len(thresholds)+1)
	for _, row := range rowIndices {
		bin := histogramBin(thresholds, b.set.rows[row].values[feature])
		buckets[bin].count++
		buckets[bin].gradient += b.derivatives.gradients[row]
		buckets[bin].hessian += b.derivatives.hessians[row]
	}

	return buckets
}

func (b histogramTreeBuilder) leafValue(
	statistics histogramNodeStatistics,
	bounds histogramValueBounds,
) float64 {
	denominator := statistics.hessian + b.options.L2Regularization
	if denominator <= 0 {
		return 0
	}
	value := statistics.gradient / denominator

	return max(bounds.lower, min(value, bounds.upper))
}

func (b histogramTreeBuilder) objective(
	statistics histogramNodeStatistics,
	value float64,
) float64 {
	return 2*statistics.gradient*value -
		(statistics.hessian+b.options.L2Regularization)*value*value
}

func (b histogramTreeBuilder) partitionRows(
	rowIndices []int,
	split histogramSplitCandidate,
) ([]int, []int) {
	left := make([]int, 0, len(rowIndices))
	right := make([]int, 0, len(rowIndices))
	for _, row := range rowIndices {
		if b.set.rows[row].values[split.featureIndex] <= split.threshold {
			left = append(left, row)
		} else {
			right = append(right, row)
		}
	}

	return left, right
}

func addHistogramStatistics(
	statistics histogramNodeStatistics,
	bucket histogramBucket,
) histogramNodeStatistics {
	statistics.count += bucket.count
	statistics.gradient += bucket.gradient
	statistics.hessian += bucket.hessian

	return statistics
}

func subtractHistogramStatistics(
	total histogramNodeStatistics,
	left histogramNodeStatistics,
) histogramNodeStatistics {
	return histogramNodeStatistics{
		count:    total.count - left.count,
		gradient: total.gradient - left.gradient,
		hessian:  total.hessian - left.hessian,
	}
}
