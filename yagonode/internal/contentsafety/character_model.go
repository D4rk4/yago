package contentsafety

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

const (
	characterModelFormat        = "yago-content-safety-chargram-logistic-v1"
	maximumCoefficientMagnitude = 32.0
)

type CharacterModel struct {
	weights              []float64
	intercept            float64
	calibrationSlope     float64
	calibrationIntercept float64
}

type characterModelDocument struct {
	Format               string    `json:"format"`
	FeatureSpaceSize     int       `json:"feature_space_size"`
	MinimumGramLength    int       `json:"minimum_gram_length"`
	MaximumGramLength    int       `json:"maximum_gram_length"`
	Weights              []float64 `json:"weights"`
	Intercept            float64   `json:"intercept"`
	CalibrationSlope     float64   `json:"calibration_slope"`
	CalibrationIntercept float64   `json:"calibration_intercept"`
}

func (m CharacterModel) Validate() error {
	if len(m.weights) != CharacterFeatureSpaceSize {
		return fmt.Errorf(
			"character model feature space must contain %d weights",
			CharacterFeatureSpaceSize,
		)
	}
	for _, weight := range m.weights {
		if !boundedCoefficient(weight) {
			return fmt.Errorf("character model weights must be finite and bounded")
		}
	}
	if !boundedCoefficient(m.intercept) {
		return fmt.Errorf("character model intercept must be finite and bounded")
	}
	if !boundedCoefficient(m.calibrationIntercept) {
		return fmt.Errorf("character model calibration intercept must be finite and bounded")
	}
	if !boundedCoefficient(m.calibrationSlope) || m.calibrationSlope <= 0 {
		return fmt.Errorf("character model calibration slope must be positive and bounded")
	}

	return nil
}

func (m CharacterModel) Classify(text string) Evidence {
	if m.Validate() != nil {
		return Evidence{Rating: Unknown}
	}
	features := characterFeatures(text)
	if len(features) == 0 {
		return Evidence{Rating: Unknown}
	}
	rawScore := m.intercept
	for _, feature := range features {
		rawScore += m.weights[feature.position] * feature.value
	}
	probability := logistic(m.calibrationSlope*rawScore + m.calibrationIntercept)

	return probabilityEvidence(probability)
}

func (m CharacterModel) MarshalJSON() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	encoded := append([]byte(`{"format":`), strconv.Quote(characterModelFormat)...)
	encoded = append(encoded, `,"feature_space_size":`...)
	encoded = strconv.AppendInt(encoded, CharacterFeatureSpaceSize, 10)
	encoded = append(encoded, `,"minimum_gram_length":`...)
	encoded = strconv.AppendInt(encoded, minimumCharacterGramLength, 10)
	encoded = append(encoded, `,"maximum_gram_length":`...)
	encoded = strconv.AppendInt(encoded, maximumCharacterGramLength, 10)
	encoded = append(encoded, `,"weights":[`...)
	for position, weight := range m.weights {
		if position > 0 {
			encoded = append(encoded, ',')
		}
		encoded = strconv.AppendFloat(encoded, weight, 'g', -1, 64)
	}
	encoded = append(encoded, `],"intercept":`...)
	encoded = strconv.AppendFloat(encoded, m.intercept, 'g', -1, 64)
	encoded = append(encoded, `,"calibration_slope":`...)
	encoded = strconv.AppendFloat(encoded, m.calibrationSlope, 'g', -1, 64)
	encoded = append(encoded, `,"calibration_intercept":`...)
	encoded = strconv.AppendFloat(encoded, m.calibrationIntercept, 'g', -1, 64)
	encoded = append(encoded, '}')

	return encoded, nil
}

func (m *CharacterModel) UnmarshalJSON(encoded []byte) error {
	var document characterModelDocument
	if err := json.Unmarshal(encoded, &document); err != nil {
		return fmt.Errorf("decode character content-safety model: %w", err)
	}
	if document.Format != characterModelFormat {
		return fmt.Errorf("unsupported character content-safety model format %q", document.Format)
	}
	if document.FeatureSpaceSize != CharacterFeatureSpaceSize ||
		document.MinimumGramLength != minimumCharacterGramLength ||
		document.MaximumGramLength != maximumCharacterGramLength {
		return fmt.Errorf("character content-safety model shape is unsupported")
	}
	candidate := CharacterModel{
		weights:              append([]float64(nil), document.Weights...),
		intercept:            document.Intercept,
		calibrationSlope:     document.CalibrationSlope,
		calibrationIntercept: document.CalibrationIntercept,
	}
	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("validate character content-safety model: %w", err)
	}
	*m = candidate

	return nil
}

func boundedCoefficient(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) &&
		math.Abs(value) <= maximumCoefficientMagnitude
}

func logistic(value float64) float64 {
	if value >= 0 {
		decay := math.Exp(-value)

		return 1 / (1 + decay)
	}
	growth := math.Exp(value)

	return growth / (1 + growth)
}
