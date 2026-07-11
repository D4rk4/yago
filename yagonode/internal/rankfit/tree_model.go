package rankfit

import "slices"

const (
	histogramLambdaMARTLegacyFormat = "yago-histogram-lambdamart-v1"
	histogramLambdaMARTFormat       = "yago-histogram-lambdamart-v2"
)

type HistogramLambdaMARTModel struct {
	featureDefinitions []FeatureDefinition
	learningRate       float64
	trees              []histogramRankingTree
	missingPolicy      missingEvidencePolicy
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
	FeatureName       string
	Known             bool
	TerminatedMissing bool
	Value             float64
	Threshold         float64
	WentLeft          bool
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
	return newHistogramLambdaMARTModelWithPolicy(
		featureDefinitions,
		learningRate,
		trees,
		missingEvidenceNeutral,
	)
}

func newHistogramLambdaMARTModelWithPolicy(
	featureDefinitions []FeatureDefinition,
	learningRate float64,
	trees []histogramRankingTree,
	missingPolicy missingEvidencePolicy,
) (HistogramLambdaMARTModel, error) {
	model := HistogramLambdaMARTModel{
		featureDefinitions: append([]FeatureDefinition(nil), featureDefinitions...),
		learningRate:       learningRate,
		trees:              cloneHistogramRankingTrees(trees),
		missingPolicy:      missingPolicy,
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

	return normalizeQueryGroup(group, len(m.featureDefinitions), m.missingPolicy)
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
		value, decisions := tree.evaluate(
			example,
			m.featureDefinitions,
			m.missingPolicy,
			includePath,
		)
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
	example normalizedRankingExample,
	featureDefinitions []FeatureDefinition,
	missingPolicy missingEvidencePolicy,
	includePath bool,
) (float64, []HistogramTreeDecision) {
	node := t.root
	var decisions []HistogramTreeDecision
	for !node.leaf {
		known := example.known[node.featureIndex]
		if !known && missingPolicy == missingEvidenceNeutral {
			if includePath {
				decisions = append(decisions, HistogramTreeDecision{
					FeatureName:       featureDefinitions[node.featureIndex].Name,
					TerminatedMissing: true,
					Threshold:         node.threshold,
				})
			}

			return 0, decisions
		}
		wentLeft := example.values[node.featureIndex] <= node.threshold
		if includePath {
			decisions = append(decisions, HistogramTreeDecision{
				FeatureName: featureDefinitions[node.featureIndex].Name,
				Known:       known,
				Value:       example.values[node.featureIndex],
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
