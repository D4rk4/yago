package contentsafety

import (
	"context"
	"fmt"
	"math"
	"slices"
)

const (
	characterTrainingIterations    = 80
	characterCalibrationIterations = 80
	characterLearningRate          = 0.4
	characterRegularization        = 0.001
	minimumDocumentsPerRating      = 2
)

type LabeledDocument struct {
	Text   string
	Rating Rating
}

type preparedDocument struct {
	features []characterFeature
	target   float64
}

func TrainCharacterModel(
	ctx context.Context,
	documents []LabeledDocument,
) (CharacterModel, error) {
	if ctx == nil {
		return CharacterModel{}, fmt.Errorf("character model training context must not be nil")
	}
	ordered, err := validateLabeledDocuments(documents)
	if err != nil {
		return CharacterModel{}, err
	}
	if err := ctx.Err(); err != nil {
		return CharacterModel{}, fmt.Errorf("train character content-safety model: %w", err)
	}
	prepared, err := prepareDocuments(ctx, ordered)
	if err != nil {
		return CharacterModel{}, err
	}
	training, calibration := splitPreparedDocuments(prepared)
	weights, intercept, err := fitCharacterLogistic(ctx, training)
	if err != nil {
		return CharacterModel{}, err
	}
	slope, calibrationIntercept, err := fitCharacterCalibration(
		ctx,
		calibration,
		weights,
		intercept,
	)
	if err != nil {
		return CharacterModel{}, err
	}

	return CharacterModel{
		weights:              weights,
		intercept:            intercept,
		calibrationSlope:     slope,
		calibrationIntercept: calibrationIntercept,
	}, nil
}

func validateLabeledDocuments(documents []LabeledDocument) ([]LabeledDocument, error) {
	if len(documents) < 2*minimumDocumentsPerRating || len(documents) > MaximumLabeledDocuments {
		return nil, fmt.Errorf(
			"character model training requires between %d and %d labeled documents",
			2*minimumDocumentsPerRating,
			MaximumLabeledDocuments,
		)
	}
	ordered := append([]LabeledDocument(nil), documents...)
	generalDocuments := 0
	explicitDocuments := 0
	for _, document := range ordered {
		if !boundedDocument(document.Text) {
			return nil, fmt.Errorf(
				"labeled document text must contain between 1 and %d runes",
				MaximumLabeledDocumentRunes,
			)
		}
		switch document.Rating {
		case General:
			generalDocuments++
		case Explicit:
			explicitDocuments++
		default:
			return nil, fmt.Errorf("labeled document rating must be General or Explicit")
		}
	}
	if generalDocuments < minimumDocumentsPerRating ||
		explicitDocuments < minimumDocumentsPerRating {
		return nil, fmt.Errorf(
			"character model training requires at least two documents per rating",
		)
	}
	slices.SortFunc(ordered, compareLabeledDocuments)

	return ordered, nil
}

func compareLabeledDocuments(left, right LabeledDocument) int {
	if left.Rating < right.Rating {
		return -1
	}
	if left.Rating > right.Rating {
		return 1
	}
	if left.Text < right.Text {
		return -1
	}
	if left.Text > right.Text {
		return 1
	}

	return 0
}

func prepareDocuments(
	ctx context.Context,
	documents []LabeledDocument,
) ([]preparedDocument, error) {
	prepared := make([]preparedDocument, len(documents))
	for index, document := range documents {
		features, err := characterFeaturesWithContext(ctx, document.Text)
		if err != nil {
			return nil, fmt.Errorf("prepare character model training data: %w", err)
		}
		if len(features) == 0 {
			return nil, fmt.Errorf("labeled document must contain a character n-gram")
		}
		target := 0.0
		if document.Rating == Explicit {
			target = 1
		}
		prepared[index] = preparedDocument{features: features, target: target}
	}

	return prepared, nil
}

func splitPreparedDocuments(documents []preparedDocument) ([]preparedDocument, []preparedDocument) {
	generalBoundary := 0
	for generalBoundary < len(documents) && documents[generalBoundary].target == 0 {
		generalBoundary++
	}
	generalCalibration := max(1, generalBoundary/5)
	explicitCalibration := max(1, (len(documents)-generalBoundary)/5)
	training := make([]preparedDocument, 0, len(documents)-generalCalibration-explicitCalibration)
	calibration := make([]preparedDocument, 0, generalCalibration+explicitCalibration)
	training = append(training, documents[:generalBoundary-generalCalibration]...)
	calibration = append(
		calibration,
		documents[generalBoundary-generalCalibration:generalBoundary]...)
	training = append(training, documents[generalBoundary:len(documents)-explicitCalibration]...)
	calibration = append(calibration, documents[len(documents)-explicitCalibration:]...)

	return training, calibration
}

func fitCharacterLogistic(
	ctx context.Context,
	documents []preparedDocument,
) ([]float64, float64, error) {
	weights := make([]float64, CharacterFeatureSpaceSize)
	intercept := 0.0
	for iteration := range characterTrainingIterations {
		if err := ctx.Err(); err != nil {
			return nil, 0, fmt.Errorf("fit character content-safety model: %w", err)
		}
		gradient := make([]float64, CharacterFeatureSpaceSize)
		interceptGradient := 0.0
		for _, document := range documents {
			difference := document.target - logistic(
				characterScore(weights, intercept, document.features),
			)
			interceptGradient += difference
			for _, feature := range document.features {
				gradient[feature.position] += difference * feature.value
			}
		}
		rate := characterLearningRate / math.Sqrt(float64(iteration+1))
		scale := 1 / float64(len(documents))
		for position := range weights {
			update := scale*gradient[position] - characterRegularization*weights[position]
			weights[position] = boundedUpdate(weights[position], rate*update)
		}
		intercept = boundedUpdate(intercept, rate*scale*interceptGradient)
	}

	return weights, intercept, nil
}

func fitCharacterCalibration(
	ctx context.Context,
	documents []preparedDocument,
	weights []float64,
	intercept float64,
) (float64, float64, error) {
	positiveDocuments := 0
	for _, document := range documents {
		if document.target == 1 {
			positiveDocuments++
		}
	}
	negativeDocuments := len(documents) - positiveDocuments
	positiveTarget := float64(positiveDocuments+1) / float64(positiveDocuments+2)
	negativeTarget := 1 / float64(negativeDocuments+2)
	slope := 1.0
	calibrationIntercept := math.Log(
		float64(positiveDocuments+1) / float64(negativeDocuments+1),
	)
	for iteration := range characterCalibrationIterations {
		if err := ctx.Err(); err != nil {
			return 0, 0, fmt.Errorf("calibrate character content-safety model: %w", err)
		}
		slopeGradient := 0.0
		interceptGradient := 0.0
		for _, document := range documents {
			rawScore := characterScore(weights, intercept, document.features)
			target := negativeTarget
			if document.target == 1 {
				target = positiveTarget
			}
			difference := target - logistic(slope*rawScore+calibrationIntercept)
			slopeGradient += difference * rawScore
			interceptGradient += difference
		}
		rate := 0.2 / math.Sqrt(float64(iteration+1))
		scale := 1 / float64(len(documents))
		slope = min(max(slope+rate*scale*slopeGradient, 0.01), maximumCoefficientMagnitude)
		calibrationIntercept = boundedUpdate(
			calibrationIntercept,
			rate*scale*interceptGradient,
		)
	}

	return slope, calibrationIntercept, nil
}

func characterScore(
	weights []float64,
	intercept float64,
	features []characterFeature,
) float64 {
	score := intercept
	for _, feature := range features {
		score += weights[feature.position] * feature.value
	}

	return score
}

func boundedUpdate(value, update float64) float64 {
	return min(max(value+update, -maximumCoefficientMagnitude), maximumCoefficientMagnitude)
}
