package rankingtrain

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/learnedrank"
	"github.com/D4rk4/yago/yagonode/internal/rankfit"
)

type trainedRankingModel struct {
	family    ModelFamily
	linear    rankfit.LinearLambdaRankModel
	histogram rankfit.HistogramLambdaMARTModel
}

func trainedRankingModelForSnapshot(
	snapshot learnedrank.Snapshot,
) (trainedRankingModel, error) {
	switch snapshot.Kind() {
	case learnedrank.ModelLinearLambdaRank:
		model, _ := snapshot.LinearModel()

		return trainedRankingModel{family: FamilyLinearLambdaRank, linear: model}, nil
	case learnedrank.ModelHistogramLambdaMART:
		model, _ := snapshot.HistogramModel()

		return trainedRankingModel{family: FamilyHistogramLambdaMART, histogram: model}, nil
	default:
		return trainedRankingModel{}, fmt.Errorf(
			"model kind %q is unsupported",
			snapshot.Kind(),
		)
	}
}

func trainRankingModel(
	ctx context.Context,
	family ModelFamily,
	revision string,
	datasets []queryDataset,
) (trainedRankingModel, learnedrank.Snapshot, TrainingReport, error) {
	groups := make([]rankfit.QueryGroup, 0, len(datasets))
	for _, dataset := range datasets {
		if dataset.hasGroup {
			groups = append(groups, dataset.group)
		}
	}
	preferencePairs, err := countPreferencePairs(ctx, groups)
	if err != nil {
		return trainedRankingModel{}, learnedrank.Snapshot{}, TrainingReport{}, err
	}
	if preferencePairs == 0 {
		return trainedRankingModel{}, learnedrank.Snapshot{}, TrainingReport{},
			fmt.Errorf("training split has no preference evidence")
	}
	definitions := learnedrank.FeatureDefinitions()
	switch family {
	case FamilyLinearLambdaRank:
		model, report, err := rankfit.TrainLinearLambdaRank(
			ctx,
			definitions,
			groups,
			rankfit.DefaultLinearLambdaRankTrainingOptions(),
		)
		if err != nil {
			return trainedRankingModel{}, learnedrank.Snapshot{}, TrainingReport{},
				fmt.Errorf("train linear LambdaRank: %w", err)
		}
		snapshot, err := learnedrank.NewLinearSnapshot(revision, model)
		if err != nil {
			return trainedRankingModel{}, learnedrank.Snapshot{}, TrainingReport{},
				fmt.Errorf("build linear LambdaRank snapshot: %w", err)
		}

		return trainedRankingModel{family: family, linear: model}, snapshot, TrainingReport{
			Family:          family,
			PreferencePairs: report.PreferencePairs,
			Iterations:      report.Iterations,
		}, nil
	case FamilyHistogramLambdaMART:
		model, report, err := rankfit.TrainHistogramLambdaMART(
			ctx,
			definitions,
			groups,
			rankfit.DefaultHistogramLambdaMARTTrainingOptions(),
		)
		if err != nil {
			return trainedRankingModel{}, learnedrank.Snapshot{}, TrainingReport{},
				fmt.Errorf("train histogram LambdaMART: %w", err)
		}
		snapshot, err := learnedrank.NewHistogramSnapshot(revision, model)
		if err != nil {
			return trainedRankingModel{}, learnedrank.Snapshot{}, TrainingReport{},
				fmt.Errorf("build histogram LambdaMART snapshot: %w", err)
		}

		return trainedRankingModel{family: family, histogram: model}, snapshot, TrainingReport{
			Family:          family,
			PreferencePairs: report.PreferencePairs,
			Trees:           report.Trees,
		}, nil
	default:
		return trainedRankingModel{}, learnedrank.Snapshot{}, TrainingReport{},
			fmt.Errorf("model family %q is unsupported", family)
	}
}

func countPreferencePairs(ctx context.Context, groups []rankfit.QueryGroup) (int, error) {
	pairs := 0
	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return 0, fmt.Errorf("count preference evidence: %w", err)
		}
		groupPairs, err := countGroupPreferencePairs(
			ctx,
			group.Examples(),
			MaximumPreferencePairs-pairs,
		)
		if err != nil {
			return 0, err
		}
		pairs += groupPairs
	}

	return pairs, nil
}

func countGroupPreferencePairs(
	ctx context.Context,
	examples []rankfit.RankingExample,
	remaining int,
) (int, error) {
	pairs := 0
	for left := range examples {
		if err := ctx.Err(); err != nil {
			return 0, fmt.Errorf("count preference evidence: %w", err)
		}
		for right := left + 1; right < len(examples); right++ {
			if right&63 == 0 {
				if err := ctx.Err(); err != nil {
					return 0, fmt.Errorf("count preference evidence: %w", err)
				}
			}
			if examples[left].Relevance() == examples[right].Relevance() {
				continue
			}
			if pairs == remaining {
				return 0, fmt.Errorf(
					"preference evidence exceeds the limit of %d pairs",
					MaximumPreferencePairs,
				)
			}
			pairs++
		}
	}

	return pairs, nil
}

func (m trainedRankingModel) predict(group rankfit.QueryGroup) ([]rankfit.RankedDocument, error) {
	switch m.family {
	case FamilyLinearLambdaRank:
		predictions, err := m.linear.Predict(group)
		if err != nil {
			return nil, fmt.Errorf("predict linear LambdaRank: %w", err)
		}

		return predictions, nil
	case FamilyHistogramLambdaMART:
		predictions, err := m.histogram.Predict(group)
		if err != nil {
			return nil, fmt.Errorf("predict histogram LambdaMART: %w", err)
		}

		return predictions, nil
	default:
		return nil, fmt.Errorf("model family %q is unsupported", m.family)
	}
}
