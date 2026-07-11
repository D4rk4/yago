package rankfit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

type histogramLambdaMARTModelDocument struct {
	Format             string                         `json:"format"`
	FeatureDefinitions []FeatureDefinition            `json:"features"`
	LearningRate       float64                        `json:"learning_rate"`
	Trees              []histogramRankingTreeDocument `json:"trees"`
}

type histogramRankingTreeDocument struct {
	InteractionGroup      string                    `json:"interaction_group"`
	AllowedFeatureIndices []int                     `json:"allowed_feature_indices"`
	Root                  histogramTreeNodeDocument `json:"root"`
}

type histogramTreeNodeDocument struct {
	LeafValue    *float64                   `json:"leaf,omitempty"`
	FeatureIndex *int                       `json:"feature,omitempty"`
	Threshold    *float64                   `json:"threshold,omitempty"`
	Left         *histogramTreeNodeDocument `json:"left,omitempty"`
	Right        *histogramTreeNodeDocument `json:"right,omitempty"`
}

func (m HistogramLambdaMARTModel) MarshalJSON() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	encoded := append([]byte(`{"format":`), strconv.Quote(histogramLambdaMARTFormat)...)
	encoded = append(encoded, `,"features":[`...)
	for index, definition := range m.featureDefinitions {
		if index > 0 {
			encoded = append(encoded, ',')
		}
		encoded = append(encoded, `{"name":`...)
		encoded = append(encoded, strconv.Quote(definition.Name)...)
		encoded = append(encoded, `,"direction":`...)
		encoded = strconv.AppendInt(encoded, int64(definition.Direction), 10)
		encoded = append(encoded, '}')
	}
	encoded = append(encoded, `],"learning_rate":`...)
	encoded = strconv.AppendFloat(encoded, m.learningRate, 'g', -1, 64)
	encoded = append(encoded, `,"trees":[`...)
	for index, tree := range m.trees {
		if index > 0 {
			encoded = append(encoded, ',')
		}
		encoded = appendHistogramTreeJSON(encoded, tree)
	}
	encoded = append(encoded, ']', '}')

	return encoded, nil
}

func (m *HistogramLambdaMARTModel) UnmarshalJSON(data []byte) error {
	var document histogramLambdaMARTModelDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return fmt.Errorf("decode histogram LambdaMART model: %w", err)
	}
	if err := requireHistogramJSONEnd(decoder); err != nil {
		return err
	}
	if document.Format != histogramLambdaMARTFormat {
		return fmt.Errorf("unsupported histogram LambdaMART model format %q", document.Format)
	}
	trees, err := histogramTreesFromDocuments(document.Trees)
	if err != nil {
		return err
	}
	model, err := newHistogramLambdaMARTModel(
		document.FeatureDefinitions,
		document.LearningRate,
		trees,
	)
	if err != nil {
		return fmt.Errorf("validate histogram LambdaMART model: %w", err)
	}
	*m = model

	return nil
}

func requireHistogramJSONEnd(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode histogram LambdaMART model: trailing JSON value")
		}

		return fmt.Errorf("decode histogram LambdaMART model: %w", err)
	}

	return nil
}

func appendHistogramTreeJSON(encoded []byte, tree histogramRankingTree) []byte {
	encoded = append(encoded, `{"interaction_group":`...)
	encoded = append(encoded, strconv.Quote(tree.interactionGroup)...)
	encoded = append(encoded, `,"allowed_feature_indices":[`...)
	for index, feature := range tree.allowedFeatureIndices {
		if index > 0 {
			encoded = append(encoded, ',')
		}
		encoded = strconv.AppendInt(encoded, int64(feature), 10)
	}
	encoded = append(encoded, `],"root":`...)
	encoded = appendHistogramNodeJSON(encoded, tree.root)

	return append(encoded, '}')
}

func appendHistogramNodeJSON(encoded []byte, node *histogramTreeNode) []byte {
	if node.leaf {
		encoded = append(encoded, `{"leaf":`...)
		encoded = strconv.AppendFloat(encoded, node.value, 'g', -1, 64)

		return append(encoded, '}')
	}
	encoded = append(encoded, `{"feature":`...)
	encoded = strconv.AppendInt(encoded, int64(node.featureIndex), 10)
	encoded = append(encoded, `,"threshold":`...)
	encoded = strconv.AppendFloat(encoded, node.threshold, 'g', -1, 64)
	encoded = append(encoded, `,"left":`...)
	encoded = appendHistogramNodeJSON(encoded, node.left)
	encoded = append(encoded, `,"right":`...)
	encoded = appendHistogramNodeJSON(encoded, node.right)

	return append(encoded, '}')
}

func histogramTreesFromDocuments(
	documents []histogramRankingTreeDocument,
) ([]histogramRankingTree, error) {
	if len(documents) > maximumHistogramTrees {
		return nil, fmt.Errorf("model trees must not exceed %d", maximumHistogramTrees)
	}
	trees := make([]histogramRankingTree, len(documents))
	for index, document := range documents {
		root, err := histogramNodeFromDocument(document.Root)
		if err != nil {
			return nil, fmt.Errorf("decode tree %d: %w", index+1, err)
		}
		trees[index] = histogramRankingTree{
			interactionGroup:      document.InteractionGroup,
			allowedFeatureIndices: append([]int(nil), document.AllowedFeatureIndices...),
			root:                  root,
		}
	}

	return trees, nil
}

func histogramNodeFromDocument(
	document histogramTreeNodeDocument,
) (*histogramTreeNode, error) {
	return histogramNodeFromDocumentAtDepth(document, 0)
}

func histogramNodeFromDocumentAtDepth(
	document histogramTreeNodeDocument,
	depth int,
) (*histogramTreeNode, error) {
	if depth > maximumHistogramDepth {
		return nil, fmt.Errorf("tree depth must not exceed %d", maximumHistogramDepth)
	}
	if document.LeafValue != nil {
		if document.FeatureIndex != nil || document.Threshold != nil ||
			document.Left != nil || document.Right != nil {
			return nil, fmt.Errorf("leaf document is ambiguous")
		}

		return &histogramTreeNode{leaf: true, value: *document.LeafValue}, nil
	}
	if document.FeatureIndex == nil || document.Threshold == nil ||
		document.Left == nil || document.Right == nil {
		return nil, fmt.Errorf("split document is incomplete")
	}
	left, err := histogramNodeFromDocumentAtDepth(*document.Left, depth+1)
	if err != nil {
		return nil, err
	}
	right, err := histogramNodeFromDocumentAtDepth(*document.Right, depth+1)
	if err != nil {
		return nil, err
	}

	return &histogramTreeNode{
		featureIndex: *document.FeatureIndex,
		threshold:    *document.Threshold,
		left:         left,
		right:        right,
	}, nil
}
