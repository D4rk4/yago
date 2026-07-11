package rankfit

import "slices"

const histogramLambdaMARTFormat = "yago-histogram-lambdamart-v1"

type HistogramLambdaMARTModel struct {
	featureDefinitions []FeatureDefinition
	learningRate       float64
	trees              []histogramRankingTree
}

type histogramRankingTree struct {
	interactionGroup      string
	allowedFeatureIndices []int
	root                  *histogramTreeNode
}

type histogramTreeNode struct {
	leaf         bool
	value        float64
	featureIndex int
	threshold    float64
	left         *histogramTreeNode
	right        *histogramTreeNode
}

type HistogramTreeDecision struct {
	FeatureName string
	Value       float64
	Threshold   float64
	WentLeft    bool
}

type HistogramTreeContribution struct {
	TreeIndex        int
	InteractionGroup string
	Contribution     float64
	Decisions        []HistogramTreeDecision
}

type HistogramRankingExplanation struct {
	DocumentIdentifier string
	Score              float64
	Rank               int
	TreeContributions  []HistogramTreeContribution
}

type histogramEvaluation struct {
	documentIdentifier string
	score              float64
	contributions      []HistogramTreeContribution
}

func newHistogramLambdaMARTModel(
	featureDefinitions []FeatureDefinition,
	learningRate float64,
	trees []histogramRankingTree,
) (HistogramLambdaMARTModel, error) {
	model := HistogramLambdaMARTModel{
		featureDefinitions: append([]FeatureDefinition(nil), featureDefinitions...),
		learningRate:       learningRate,
		trees:              cloneHistogramRankingTrees(trees),
	}
	if err := model.Validate(); err != nil {
		return HistogramLambdaMARTModel{}, err
	}

	return model, nil
}

func (m HistogramLambdaMARTModel) FeatureDefinitions() []FeatureDefinition {
	return append([]FeatureDefinition(nil), m.featureDefinitions...)
}

func (m HistogramLambdaMARTModel) LearningRate() float64 {
	return m.learningRate
}

func (m HistogramLambdaMARTModel) TreeCount() int {
	return len(m.trees)
}

func (m HistogramLambdaMARTModel) Predict(group QueryGroup) ([]RankedDocument, error) {
	normalized, err := m.normalizedGroup(group)
	if err != nil {
		return nil, err
	}
	evaluations := m.evaluateHistogramGroup(normalized, false)
	predictions := make([]RankedDocument, len(evaluations))
	for index, evaluation := range evaluations {
		predictions[index] = RankedDocument{
			DocumentIdentifier: evaluation.documentIdentifier,
			Score:              evaluation.score,
			Rank:               index + 1,
		}
	}

	return predictions, nil
}

func (m HistogramLambdaMARTModel) Explain(
	group QueryGroup,
) ([]HistogramRankingExplanation, error) {
	normalized, err := m.normalizedGroup(group)
	if err != nil {
		return nil, err
	}
	evaluations := m.evaluateHistogramGroup(normalized, true)
	explanations := make([]HistogramRankingExplanation, len(evaluations))
	for index, evaluation := range evaluations {
		explanations[index] = HistogramRankingExplanation{
			DocumentIdentifier: evaluation.documentIdentifier,
			Score:              evaluation.score,
			Rank:               index + 1,
			TreeContributions:  evaluation.contributions,
		}
	}

	return explanations, nil
}

func (m HistogramLambdaMARTModel) normalizedGroup(
	group QueryGroup,
) (normalizedQueryGroup, error) {
	if err := m.Validate(); err != nil {
		return normalizedQueryGroup{}, err
	}

	return normalizeQueryGroup(group, len(m.featureDefinitions))
}

func (m HistogramLambdaMARTModel) evaluateHistogramGroup(
	group normalizedQueryGroup,
	includePath bool,
) []histogramEvaluation {
	evaluations := make([]histogramEvaluation, len(group.examples))
	for index, example := range group.examples {
		evaluations[index] = m.evaluateHistogramExample(example, includePath)
	}
	slices.SortStableFunc(evaluations, func(left, right histogramEvaluation) int {
		if left.score > right.score {
			return -1
		}
		if left.score < right.score {
			return 1
		}

		return 0
	})

	return evaluations
}

func (m HistogramLambdaMARTModel) evaluateHistogramExample(
	example normalizedRankingExample,
	includePath bool,
) histogramEvaluation {
	evaluation := histogramEvaluation{documentIdentifier: example.documentIdentifier}
	if includePath {
		evaluation.contributions = make([]HistogramTreeContribution, 0, len(m.trees))
	}
	for index, tree := range m.trees {
		value, decisions := tree.evaluate(example.values, m.featureDefinitions, includePath)
		contribution := m.learningRate * value
		evaluation.score += contribution
		if includePath {
			evaluation.contributions = append(
				evaluation.contributions,
				HistogramTreeContribution{
					TreeIndex:        index + 1,
					InteractionGroup: tree.interactionGroup,
					Contribution:     contribution,
					Decisions:        decisions,
				},
			)
		}
	}

	return evaluation
}

func (t histogramRankingTree) evaluate(
	values []float64,
	featureDefinitions []FeatureDefinition,
	includePath bool,
) (float64, []HistogramTreeDecision) {
	node := t.root
	var decisions []HistogramTreeDecision
	for !node.leaf {
		wentLeft := values[node.featureIndex] <= node.threshold
		if includePath {
			decisions = append(decisions, HistogramTreeDecision{
				FeatureName: featureDefinitions[node.featureIndex].Name,
				Value:       values[node.featureIndex],
				Threshold:   node.threshold,
				WentLeft:    wentLeft,
			})
		}
		if wentLeft {
			node = node.left
		} else {
			node = node.right
		}
	}

	return node.value, decisions
}
