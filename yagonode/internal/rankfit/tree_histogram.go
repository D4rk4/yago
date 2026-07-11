package rankfit

import (
	"slices"
	"sort"
)

type histogramTrainingRow struct {
	queryIndex   int
	exampleIndex int
	values       []float64
	known        []bool
}

type histogramTrainingSet struct {
	rows       []histogramTrainingRow
	thresholds [][]float64
}

func newHistogramTrainingSet(
	groups []normalizedQueryGroup,
	dimension int,
	maximumBins int,
) histogramTrainingSet {
	rowCount := 0
	for _, group := range groups {
		rowCount += len(group.examples)
	}
	set := histogramTrainingSet{
		rows:       make([]histogramTrainingRow, 0, rowCount),
		thresholds: make([][]float64, dimension),
	}
	featureValues := make([][]float64, dimension)
	for queryIndex, group := range groups {
		for exampleIndex, example := range group.examples {
			set.rows = append(set.rows, histogramTrainingRow{
				queryIndex:   queryIndex,
				exampleIndex: exampleIndex,
				values:       example.values,
				known:        example.known,
			})
			for feature, value := range example.values {
				if example.known[feature] {
					featureValues[feature] = append(featureValues[feature], value)
				}
			}
		}
	}
	for feature, values := range featureValues {
		set.thresholds[feature] = boundedHistogramThresholds(values, maximumBins)
	}

	return set
}

func boundedHistogramThresholds(values []float64, maximumBins int) []float64 {
	ordered := append([]float64(nil), values...)
	slices.Sort(ordered)
	ordered = slices.Compact(ordered)
	if len(ordered) < 2 {
		return nil
	}
	binCount := min(maximumBins, len(ordered))
	thresholds := make([]float64, 0, binCount-1)
	for boundary := 1; boundary < binCount; boundary++ {
		index := boundary*len(ordered)/binCount - 1
		threshold := ordered[index] + (ordered[index+1]-ordered[index])/2
		if len(thresholds) == 0 || threshold > thresholds[len(thresholds)-1] {
			thresholds = append(thresholds, threshold)
		}
	}

	return thresholds
}

func histogramBin(thresholds []float64, value float64) int {
	return sort.SearchFloat64s(thresholds, value)
}

func allHistogramRowIndices(rowCount int) []int {
	indices := make([]int, rowCount)
	for index := range indices {
		indices[index] = index
	}

	return indices
}
