package rankfit

import (
	"fmt"
	"math"
)

const (
	maximumLinearFeatures          = 256
	maximumLinearExamplesPerQuery  = 2048
	maximumLinearQueries           = 10000
	maximumRelevanceGrade          = 30
	maximumLinearFeatureMagnitude  = 1e12
	maximumLinearWeightMagnitude   = 1e6
	maximumTrainingExamples        = 100000
	maximumTrainingFeatureValues   = 8000000
	maximumTrainingPreferencePairs = 1000000
)

type FeatureDirection int

const (
	FeatureUnconstrained FeatureDirection = iota
	FeatureIncreasing
	FeatureDecreasing
)

type FeatureDefinition struct {
	Name      string           `json:"name"`
	Direction FeatureDirection `json:"direction"`
}

type FeatureVector struct {
	values []float64
	known  []bool
}

func NewFeatureVector(values []float64) (FeatureVector, error) {
	known := make([]bool, len(values))
	for index := range known {
		known[index] = true
	}

	return NewFeatureVectorWithKnownValues(values, known)
}

func NewFeatureVectorWithKnownValues(values []float64, known []bool) (FeatureVector, error) {
	if len(values) == 0 || len(values) > maximumLinearFeatures {
		return FeatureVector{}, fmt.Errorf(
			"feature dimension must be between 1 and %d",
			maximumLinearFeatures,
		)
	}
	if len(known) != len(values) {
		return FeatureVector{}, fmt.Errorf("feature presence dimension differs from values")
	}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) ||
			math.Abs(value) > maximumLinearFeatureMagnitude {
			return FeatureVector{}, fmt.Errorf("feature values must be finite and bounded")
		}
	}

	return FeatureVector{
		values: append([]float64(nil), values...),
		known:  append([]bool(nil), known...),
	}, nil
}

func (v FeatureVector) Dimension() int {
	return len(v.values)
}

func (v FeatureVector) Values() []float64 {
	return append([]float64(nil), v.values...)
}

func (v FeatureVector) Known(index int) bool {
	return index >= 0 && index < len(v.known) && v.known[index]
}

func (v FeatureVector) validate() error {
	_, err := NewFeatureVectorWithKnownValues(v.values, v.known)

	return err
}

type RankingExample struct {
	documentIdentifier string
	relevance          int
	features           FeatureVector
}

func NewRankingExample(
	documentIdentifier string,
	relevance int,
	features FeatureVector,
) (RankingExample, error) {
	if documentIdentifier == "" {
		return RankingExample{}, fmt.Errorf("document identifier must not be empty")
	}
	if relevance < 0 || relevance > maximumRelevanceGrade {
		return RankingExample{}, fmt.Errorf(
			"relevance must be between 0 and %d",
			maximumRelevanceGrade,
		)
	}
	if err := features.validate(); err != nil {
		return RankingExample{}, fmt.Errorf("document %q features: %w", documentIdentifier, err)
	}

	return RankingExample{
		documentIdentifier: documentIdentifier,
		relevance:          relevance,
		features:           cloneFeatureVector(features),
	}, nil
}

func (e RankingExample) DocumentIdentifier() string {
	return e.documentIdentifier
}

func (e RankingExample) Relevance() int {
	return e.relevance
}

func (e RankingExample) Features() FeatureVector {
	return cloneFeatureVector(e.features)
}

func (e RankingExample) validate() error {
	_, err := NewRankingExample(e.documentIdentifier, e.relevance, e.features)

	return err
}

type QueryGroup struct {
	queryIdentifier string
	examples        []RankingExample
}

func NewQueryGroup(queryIdentifier string, examples []RankingExample) (QueryGroup, error) {
	if queryIdentifier == "" {
		return QueryGroup{}, fmt.Errorf("query identifier must not be empty")
	}
	if len(examples) == 0 || len(examples) > maximumLinearExamplesPerQuery {
		return QueryGroup{}, fmt.Errorf(
			"query examples must be between 1 and %d",
			maximumLinearExamplesPerQuery,
		)
	}

	dimension := examples[0].features.Dimension()
	seen := make(map[string]struct{}, len(examples))
	cloned := make([]RankingExample, len(examples))
	for index, example := range examples {
		if err := example.validate(); err != nil {
			return QueryGroup{}, fmt.Errorf("query %q example: %w", queryIdentifier, err)
		}
		if example.features.Dimension() != dimension {
			return QueryGroup{}, fmt.Errorf("query %q feature dimensions differ", queryIdentifier)
		}
		if _, exists := seen[example.documentIdentifier]; exists {
			return QueryGroup{}, fmt.Errorf(
				"query %q repeats document %q",
				queryIdentifier,
				example.documentIdentifier,
			)
		}
		seen[example.documentIdentifier] = struct{}{}
		cloned[index] = cloneRankingExample(example)
	}

	return QueryGroup{queryIdentifier: queryIdentifier, examples: cloned}, nil
}

func (g QueryGroup) QueryIdentifier() string {
	return g.queryIdentifier
}

func (g QueryGroup) Examples() []RankingExample {
	cloned := make([]RankingExample, len(g.examples))
	for index, example := range g.examples {
		cloned[index] = cloneRankingExample(example)
	}

	return cloned
}

func (g QueryGroup) validate() error {
	_, err := NewQueryGroup(g.queryIdentifier, g.examples)

	return err
}

func cloneFeatureVector(vector FeatureVector) FeatureVector {
	return FeatureVector{
		values: append([]float64(nil), vector.values...),
		known:  append([]bool(nil), vector.known...),
	}
}

func cloneRankingExample(example RankingExample) RankingExample {
	return RankingExample{
		documentIdentifier: example.documentIdentifier,
		relevance:          example.relevance,
		features:           cloneFeatureVector(example.features),
	}
}

func validateFeatureDefinitions(definitions []FeatureDefinition) error {
	if len(definitions) == 0 || len(definitions) > maximumLinearFeatures {
		return fmt.Errorf("feature definitions must be between 1 and %d", maximumLinearFeatures)
	}
	seen := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		if definition.Name == "" {
			return fmt.Errorf("feature name must not be empty")
		}
		if !validFeatureName(definition.Name) {
			return fmt.Errorf("feature %q must use an ASCII identifier", definition.Name)
		}
		if definition.Direction < FeatureUnconstrained || definition.Direction > FeatureDecreasing {
			return fmt.Errorf("feature %q has an invalid direction", definition.Name)
		}
		if _, exists := seen[definition.Name]; exists {
			return fmt.Errorf("feature %q is duplicated", definition.Name)
		}
		seen[definition.Name] = struct{}{}
	}

	return nil
}

func validFeatureName(name string) bool {
	for index, character := range []byte(name) {
		letter := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
		digit := character >= '0' && character <= '9'
		separator := character == '_' || character == '-' || character == '.'
		if !letter && (!digit || index == 0) && (!separator || index == 0) {
			return false
		}
	}

	return true
}
