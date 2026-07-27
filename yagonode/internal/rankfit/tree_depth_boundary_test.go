package rankfit

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestHistogramModelAdmitsExactlyMaximumTreeDepth(t *testing.T) {
	definitions := definitionsForTest("feature")
	leafValue := 0.0
	deepest := histogramTree(
		"feature",
		[]int{0},
		balancedHistogramTree(maximumHistogramDepth, &leafValue),
	)
	model, err := newHistogramLambdaMARTModel(definitions, 0.5, []histogramRankingTree{deepest})
	if err != nil {
		t.Fatalf("tree at the depth bound: %v", err)
	}
	encoded, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("Marshal tree at the depth bound: %v", err)
	}
	var decoded HistogramLambdaMARTModel
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal tree at the depth bound: %v", err)
	}
	if !reflect.DeepEqual(decoded, model) {
		t.Fatalf("decoded deepest model differs: %#v", decoded)
	}

	beyondValue := 0.0
	beyond := histogramTree(
		"feature",
		[]int{0},
		balancedHistogramTree(maximumHistogramDepth+1, &beyondValue),
	)
	_, err = newHistogramLambdaMARTModel(definitions, 0.5, []histogramRankingTree{beyond})
	if err == nil || !strings.Contains(err.Error(), "tree depth must not exceed") {
		t.Fatalf("tree above the depth bound = %v", err)
	}
	document := histogramInvalidTreeJSON(string(appendHistogramNodeJSON(nil, beyond.root)))
	if err := json.Unmarshal([]byte(document), &decoded); err == nil ||
		!strings.Contains(err.Error(), "tree depth must not exceed") {
		t.Fatalf("tree document above the depth bound = %v", err)
	}
}

func balancedHistogramTree(depth int, nextLeafValue *float64) *histogramTreeNode {
	if depth == 0 {
		*nextLeafValue++

		return histogramLeaf(*nextLeafValue)
	}

	return histogramSplit(
		0,
		float64(depth),
		balancedHistogramTree(depth-1, nextLeafValue),
		balancedHistogramTree(depth-1, nextLeafValue),
	)
}
