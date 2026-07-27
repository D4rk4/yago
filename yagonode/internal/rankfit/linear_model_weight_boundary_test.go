package rankfit

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestLinearModelAdmitsZeroWeightOnConstrainedFeatures(t *testing.T) {
	definitions := []FeatureDefinition{
		{Name: "up", Direction: FeatureIncreasing},
		{Name: "down", Direction: FeatureDecreasing},
	}
	clamped := []float64{0, 0, 0}
	gradient := []float64{-100, 100, 100}
	projected := []FeatureDefinition{
		definitions[0],
		definitions[1],
		{Name: "free", Direction: FeatureUnconstrained},
	}
	options := DefaultLinearLambdaRankTrainingOptions()
	options.MaximumAbsoluteWeight = 0.5
	updateLinearWeights(clamped, gradient, projected, options)
	if !reflect.DeepEqual(clamped, []float64{0, 0, 0.5}) {
		t.Fatalf("projected weights = %v", clamped)
	}
	model, err := NewLinearLambdaRankModel(projected, clamped)
	if err != nil {
		t.Fatalf("projected weights were refused: %v", err)
	}
	if err := model.Validate(); err != nil {
		t.Fatalf("Validate projected weights: %v", err)
	}

	wrongIncreasing := []float64{-math.SmallestNonzeroFloat64, 0, 0}
	_, err = NewLinearLambdaRankModel(projected, wrongIncreasing)
	if err == nil || !strings.Contains(err.Error(), `feature "up" weight violates its direction`) {
		t.Fatalf("negative weight on an increasing feature = %v", err)
	}
	wrongDecreasing := []float64{0, math.SmallestNonzeroFloat64, 0}
	_, err = NewLinearLambdaRankModel(projected, wrongDecreasing)
	if err == nil ||
		!strings.Contains(err.Error(), `feature "down" weight violates its direction`) {
		t.Fatalf("positive weight on a decreasing feature = %v", err)
	}
}

func TestLinearModelAdmitsExactlyMaximumWeightMagnitude(t *testing.T) {
	definitions := []FeatureDefinition{
		{Name: "up", Direction: FeatureIncreasing},
		{Name: "down", Direction: FeatureDecreasing},
	}
	atBound := []float64{maximumLinearWeightMagnitude, -maximumLinearWeightMagnitude}
	model, err := NewLinearLambdaRankModel(definitions, atBound)
	if err != nil {
		t.Fatalf("weights at the magnitude bound: %v", err)
	}
	if !reflect.DeepEqual(model.Weights(), atBound) {
		t.Fatalf("weights at the magnitude bound = %v", model.Weights())
	}
	for index, beyond := range [][]float64{
		{math.Nextafter(maximumLinearWeightMagnitude, math.Inf(1)), -1},
		{1, math.Nextafter(-maximumLinearWeightMagnitude, math.Inf(-1))},
	} {
		if _, err := NewLinearLambdaRankModel(definitions, beyond); err == nil ||
			!strings.Contains(err.Error(), "model weights must be bounded") {
			t.Fatalf("weights above the magnitude bound %d = %v", index, err)
		}
	}
}
