package learnedrank

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/rankfit"
)

func mustLinearModel(t *testing.T, weights []float64) rankfit.LinearLambdaRankModel {
	t.Helper()
	model, err := rankfit.NewLinearLambdaRankModel(FeatureDefinitions(), weights)
	if err != nil {
		t.Fatalf("NewLinearLambdaRankModel: %v", err)
	}

	return model
}

func linearWeights(signalWeights map[int]float64) []float64 {
	weights := make([]float64, len(rankingFeatures))
	for index, weight := range signalWeights {
		weights[index] = weight
	}

	return weights
}

func mustHistogramModel(t *testing.T) rankfit.HistogramLambdaMARTModel {
	t.Helper()
	definitions := FeatureDefinitions()
	examples := make([]rankfit.RankingExample, 4)
	for index := range examples {
		values := make([]float64, len(definitions))
		values[0] = float64(index)
		vector, err := rankfit.NewFeatureVector(values)
		if err != nil {
			t.Fatalf("NewFeatureVector: %v", err)
		}
		examples[index], err = rankfit.NewRankingExample(
			string(rune('a'+index)),
			index,
			vector,
		)
		if err != nil {
			t.Fatalf("NewRankingExample: %v", err)
		}
	}
	group, err := rankfit.NewQueryGroup("training", examples)
	if err != nil {
		t.Fatalf("NewQueryGroup: %v", err)
	}
	options := rankfit.DefaultHistogramLambdaMARTTrainingOptions()
	options.MaximumTrees = 1
	options.MaximumDepth = 1
	options.MaximumBins = 4
	options.LearningRate = 1
	options.MinimumLeafExamples = 1
	options.NormalizedDiscountedCumulativeGainCutoff = 4
	options.FeatureInteractionGroups = []rankfit.FeatureInteractionGroup{
		{Name: "retrieval", FeatureIndices: []int{0}},
	}
	model, report, err := rankfit.TrainHistogramLambdaMART(
		context.Background(),
		definitions,
		[]rankfit.QueryGroup{group},
		options,
	)
	if err != nil {
		t.Fatalf("TrainHistogramLambdaMART: %v", err)
	}
	if report.Trees != 1 {
		t.Fatalf("trained trees = %d", report.Trees)
	}

	return model
}

func mustWrongHistogramModel(t *testing.T) rankfit.HistogramLambdaMARTModel {
	t.Helper()
	definitions := []rankfit.FeatureDefinition{{Name: "other"}}
	vector, err := rankfit.NewFeatureVector([]float64{1})
	if err != nil {
		t.Fatalf("NewFeatureVector: %v", err)
	}
	example, err := rankfit.NewRankingExample("document", 0, vector)
	if err != nil {
		t.Fatalf("NewRankingExample: %v", err)
	}
	group, err := rankfit.NewQueryGroup("query", []rankfit.RankingExample{example})
	if err != nil {
		t.Fatalf("NewQueryGroup: %v", err)
	}
	options := rankfit.DefaultHistogramLambdaMARTTrainingOptions()
	options.MaximumTrees = 1
	options.MaximumDepth = 1
	options.MaximumBins = 2
	options.MinimumLeafExamples = 1
	model, _, err := rankfit.TrainHistogramLambdaMART(
		context.Background(),
		definitions,
		[]rankfit.QueryGroup{group},
		options,
	)
	if err != nil {
		t.Fatalf("TrainHistogramLambdaMART: %v", err)
	}

	return model
}
