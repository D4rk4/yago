package rankfit

import (
	"fmt"
	"math"
	"slices"
)

const maximumNormalizedFeatureMagnitude = 8.0

type normalizedRankingExample struct {
	documentIdentifier string
	relevance          int
	values             []float64
	known              []bool
}

type normalizedQueryGroup struct {
	queryIdentifier string
	examples        []normalizedRankingExample
}

func normalizeQueryGroup(
	group QueryGroup,
	dimension int,
	missingPolicy missingEvidencePolicy,
) (normalizedQueryGroup, error) {
	if err := group.validate(); err != nil {
		return normalizedQueryGroup{}, err
	}
	if !missingPolicy.valid() {
		return normalizedQueryGroup{}, fmt.Errorf("missing evidence policy is invalid")
	}
	if group.examples[0].features.Dimension() != dimension {
		return normalizedQueryGroup{}, dimensionMismatchError(
			dimension,
			group.examples[0].features.Dimension(),
		)
	}

	centers := make([]float64, dimension)
	scales := make([]float64, dimension)
	for feature := range dimension {
		values := make([]float64, 0, len(group.examples))
		for _, example := range group.examples {
			if example.features.known[feature] || missingPolicy == missingEvidenceAsObservedZero {
				values = append(values, example.features.values[feature])
			}
		}
		if len(values) == 0 {
			scales[feature] = 1
			continue
		}
		centers[feature], scales[feature] = robustCenterAndScale(values)
	}

	normalized := normalizedQueryGroup{
		queryIdentifier: group.queryIdentifier,
		examples:        make([]normalizedRankingExample, len(group.examples)),
	}
	for index, example := range group.examples {
		values := make([]float64, dimension)
		for feature := range dimension {
			if !example.features.known[feature] && missingPolicy == missingEvidenceNeutral {
				continue
			}
			value := (example.features.values[feature] - centers[feature]) / scales[feature]
			values[feature] = max(
				-maximumNormalizedFeatureMagnitude,
				min(value, maximumNormalizedFeatureMagnitude),
			)
		}
		normalized.examples[index] = normalizedRankingExample{
			documentIdentifier: example.documentIdentifier,
			relevance:          example.relevance,
			values:             values,
			known:              append([]bool(nil), example.features.known...),
		}
	}

	return normalized, nil
}

func robustCenterAndScale(values []float64) (float64, float64) {
	ordered := append([]float64(nil), values...)
	slices.Sort(ordered)
	center := quantile(ordered, 0.5)
	scale := quantile(ordered, 0.75) - quantile(ordered, 0.25)
	if scale == 0 {
		scale = ordered[len(ordered)-1] - ordered[0]
	}
	if scale == 0 {
		scale = 1
	}

	return center, scale
}

func quantile(ordered []float64, probability float64) float64 {
	position := probability * float64(len(ordered)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return ordered[lower]
	}
	fraction := position - float64(lower)

	return ordered[lower] + fraction*(ordered[upper]-ordered[lower])
}
