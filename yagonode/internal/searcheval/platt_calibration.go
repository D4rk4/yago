package searcheval

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

const (
	plattMaximumIterations = 100
	plattRegularization    = 1e-6
	plattConvergence       = 1e-10
)

type CalibrationSample struct {
	Score    float64
	Relevant bool
}

type PlattModel struct {
	Slope     float64 `json:"slope"`
	Intercept float64 `json:"intercept"`
}

type plattCalibrationCoordinates struct {
	mean           float64
	scale          float64
	positiveTarget float64
	negativeTarget float64
}

type plattCalibrationParameters struct {
	slope     float64
	intercept float64
}

type plattCalibrationDerivatives struct {
	gradientSlope     float64
	gradientIntercept float64
	hessianSlope      float64
	hessianCross      float64
	hessianIntercept  float64
}

func FitPlattCalibration(samples []CalibrationSample) (PlattModel, error) {
	positive, negative, mean, scale, err := calibrationSampleSummary(samples, true)
	if err != nil {
		return PlattModel{}, err
	}
	positiveTarget := (float64(positive) + 1) / (float64(positive) + 2)
	negativeTarget := 1 / (float64(negative) + 2)
	intercept := math.Log(float64(positive) / float64(negative))
	if scale == 0 {
		model := PlattModel{Intercept: intercept}

		return model, model.Validate()
	}
	coordinates := plattCalibrationCoordinates{
		mean:           mean,
		scale:          scale,
		positiveTarget: positiveTarget,
		negativeTarget: negativeTarget,
	}
	parameters := fitPlattParameters(samples, coordinates, plattCalibrationParameters{
		intercept: intercept,
	})
	model := PlattModel{
		Slope:     parameters.slope / scale,
		Intercept: parameters.intercept - parameters.slope*mean/scale,
	}

	return model, model.Validate()
}

func fitPlattParameters(
	samples []CalibrationSample,
	coordinates plattCalibrationCoordinates,
	parameters plattCalibrationParameters,
) plattCalibrationParameters {
	for range plattMaximumIterations {
		derivatives := plattDerivatives(samples, coordinates, parameters)
		if math.Abs(derivatives.gradientSlope) < plattConvergence &&
			math.Abs(derivatives.gradientIntercept) < plattConvergence {
			break
		}
		determinant := derivatives.hessianSlope*derivatives.hessianIntercept -
			derivatives.hessianCross*derivatives.hessianCross
		deltaSlope := (derivatives.hessianIntercept*derivatives.gradientSlope -
			derivatives.hessianCross*derivatives.gradientIntercept) /
			determinant
		deltaIntercept := (derivatives.hessianSlope*derivatives.gradientIntercept -
			derivatives.hessianCross*derivatives.gradientSlope) /
			determinant
		var improved bool
		parameters, improved = plattLineSearch(
			samples,
			coordinates,
			parameters,
			deltaSlope,
			deltaIntercept,
		)
		if !improved {
			break
		}
	}

	return parameters
}

func plattDerivatives(
	samples []CalibrationSample,
	coordinates plattCalibrationCoordinates,
	parameters plattCalibrationParameters,
) plattCalibrationDerivatives {
	derivatives := plattCalibrationDerivatives{
		gradientSlope: plattRegularization * parameters.slope,
		hessianSlope:  plattRegularization,
	}
	for _, sample := range samples {
		x := (sample.Score - coordinates.mean) / coordinates.scale
		target := coordinates.negativeTarget
		if sample.Relevant {
			target = coordinates.positiveTarget
		}
		probability := sigmoid(parameters.slope*x + parameters.intercept)
		variance := probability * (1 - probability)
		difference := probability - target
		derivatives.gradientSlope += difference * x
		derivatives.gradientIntercept += difference
		derivatives.hessianSlope += variance * x * x
		derivatives.hessianCross += variance * x
		derivatives.hessianIntercept += variance
	}

	return derivatives
}

func plattLineSearch(
	samples []CalibrationSample,
	coordinates plattCalibrationCoordinates,
	parameters plattCalibrationParameters,
	deltaSlope float64,
	deltaIntercept float64,
) (plattCalibrationParameters, bool) {
	currentLoss := plattLoss(samples, coordinates, parameters)
	for step := 1.0; step >= plattConvergence; step /= 2 {
		candidate := plattCalibrationParameters{
			slope:     max(0, parameters.slope-step*deltaSlope),
			intercept: parameters.intercept - step*deltaIntercept,
		}
		if plattLoss(samples, coordinates, candidate) < currentLoss {
			return candidate, true
		}
	}

	return parameters, false
}

func (m PlattModel) Probability(score float64) float64 {
	if math.IsNaN(score) {
		return 0.5
	}
	if m.Slope <= 0 || math.IsNaN(m.Slope) {
		return sigmoid(m.Intercept)
	}

	return sigmoid(m.Slope*score + m.Intercept)
}

func (m PlattModel) Validate() error {
	if math.IsNaN(m.Slope) || math.IsInf(m.Slope, 0) || m.Slope < 0 {
		return fmt.Errorf("platt slope must be finite and non-negative")
	}
	if math.IsNaN(m.Intercept) || math.IsInf(m.Intercept, 0) {
		return fmt.Errorf("platt intercept must be finite")
	}

	return nil
}

func (m PlattModel) MarshalJSON() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	data := []byte(`{"slope":`)
	data = strconv.AppendFloat(data, m.Slope, 'g', -1, 64)
	data = append(data, `,"intercept":`...)
	data = strconv.AppendFloat(data, m.Intercept, 'g', -1, 64)
	data = append(data, '}')

	return data, nil
}

func (m *PlattModel) UnmarshalJSON(data []byte) error {
	type wire PlattModel
	var decoded wire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("decode platt model: %w", err)
	}
	model := PlattModel(decoded)
	if err := model.Validate(); err != nil {
		return err
	}
	*m = model

	return nil
}

func ParsePlattModelJSON(data []byte) (PlattModel, error) {
	var model PlattModel
	if err := json.Unmarshal(data, &model); err != nil {
		return PlattModel{}, fmt.Errorf("parse platt model: %w", err)
	}

	return model, nil
}

func calibrationSampleSummary(
	samples []CalibrationSample,
	requireBothLabels bool,
) (int, int, float64, float64, error) {
	if len(samples) == 0 {
		return 0, 0, 0, 0, fmt.Errorf("calibration needs samples")
	}
	positive := 0
	mean := 0.0
	for _, sample := range samples {
		if math.IsNaN(sample.Score) || math.IsInf(sample.Score, 0) {
			return 0, 0, 0, 0, fmt.Errorf("calibration score must be finite")
		}
		mean += sample.Score
		if sample.Relevant {
			positive++
		}
	}
	negative := len(samples) - positive
	if requireBothLabels && (positive == 0 || negative == 0) {
		return 0, 0, 0, 0, fmt.Errorf("platt calibration needs both labels")
	}
	mean /= float64(len(samples))
	variance := 0.0
	for _, sample := range samples {
		difference := sample.Score - mean
		variance += difference * difference
	}
	scale := math.Sqrt(variance / float64(len(samples)))

	return positive, negative, mean, scale, nil
}

func plattLoss(
	samples []CalibrationSample,
	coordinates plattCalibrationCoordinates,
	parameters plattCalibrationParameters,
) float64 {
	loss := plattRegularization * parameters.slope * parameters.slope / 2
	for _, sample := range samples {
		target := coordinates.negativeTarget
		if sample.Relevant {
			target = coordinates.positiveTarget
		}
		value := parameters.slope*(sample.Score-coordinates.mean)/coordinates.scale +
			parameters.intercept
		if value >= 0 {
			loss += math.Log1p(math.Exp(-value)) + (1-target)*value
		} else {
			loss += math.Log1p(math.Exp(value)) - target*value
		}
	}

	return loss
}

func sigmoid(value float64) float64 {
	if value >= 0 {
		return 1 / (1 + math.Exp(-value))
	}
	exponential := math.Exp(value)

	return exponential / (1 + exponential)
}
