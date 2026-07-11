package rankfit

import (
	"reflect"
	"testing"
)

func TestHistogramTrainingSetAndBoundedThresholds(t *testing.T) {
	groups := []normalizedQueryGroup{
		{
			queryIdentifier: "first",
			examples: []normalizedRankingExample{
				{documentIdentifier: "a", values: []float64{0, 7}},
				{documentIdentifier: "b", values: []float64{1, 7}},
			},
		},
		{
			queryIdentifier: "second",
			examples: []normalizedRankingExample{
				{documentIdentifier: "c", values: []float64{2, 7}},
				{documentIdentifier: "d", values: []float64{3, 7}},
			},
		},
	}
	set := newHistogramTrainingSet(groups, 2, 3)
	if len(set.rows) != 4 || set.rows[2].queryIndex != 1 || set.rows[2].exampleIndex != 0 ||
		!reflect.DeepEqual(set.thresholds[0], []float64{0.5, 1.5}) ||
		set.thresholds[1] != nil {
		t.Fatalf("training set = %#v", set)
	}
	if got := boundedHistogramThresholds([]float64{2, 1, 1, 0}, 32); !reflect.DeepEqual(
		got,
		[]float64{0.5, 1.5},
	) {
		t.Fatalf("thresholds = %v", got)
	}
	if boundedHistogramThresholds([]float64{1, 1}, 2) != nil {
		t.Fatalf("constant values produced thresholds")
	}
}

func TestHistogramBinsAndRowIndices(t *testing.T) {
	thresholds := []float64{0.5, 1.5}
	if histogramBin(thresholds, 0.5) != 0 || histogramBin(thresholds, 0.6) != 1 ||
		histogramBin(thresholds, 2) != 2 {
		t.Fatalf("histogram bins are incorrect")
	}
	if got := allHistogramRowIndices(4); !reflect.DeepEqual(got, []int{0, 1, 2, 3}) {
		t.Fatalf("row indices = %v", got)
	}
}
