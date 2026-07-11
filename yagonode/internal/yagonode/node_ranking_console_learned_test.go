package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/learnedrank"
	"github.com/D4rk4/yago/yagonode/internal/rankingmodel"
	"github.com/D4rk4/yago/yagonode/internal/rankingtrain"
	"github.com/D4rk4/yago/yagonode/internal/searcheval"
)

func TestRankingConsoleLearnedModelOperations(t *testing.T) {
	catalog := &rankingModelCatalogFixture{
		status: rankingmodel.Status{
			Current: rankingmodel.Revision{
				Active: true, Revision: "active-v1", Kind: learnedrank.ModelLinearLambdaRank,
			},
			Rollback: []rankingmodel.Revision{{Active: true, Revision: "previous-v1"}},
		},
		rolledBack: true,
	}
	runner := rankingTrainingRunnerFixture{outcome: rankingTrainingOutcome{
		Promoted: true,
		Counts: rankingtrain.DatasetCounts{
			Train:       rankingtrain.PartitionCounts{Queries: 6},
			Development: rankingtrain.PartitionCounts{Queries: 2},
			Test:        rankingtrain.PartitionCounts{Queries: 3},
		},
		Decision: searcheval.PromotionDecision{
			Confidence: searcheval.BootstrapConfidence{
				ObservedRelativeGain: 0.04, Confidence: 0.95,
			},
			Reasons: []string{"accepted"},
		},
	}}
	source := newRankingConsole(
		testRankingHolder(t),
		fakeRanker{},
		fakeCurated{},
		rankingConsoleLearning{trainer: runner, models: catalog},
	).(adminui.LearnedRankingSource)
	model := source.LearnedModel(context.Background())
	if model.ActiveRevision != "active-v1" ||
		model.ActiveKind != adminui.LearnedModelLinearLambdaRank ||
		!model.RollbackAvailable {
		t.Fatalf("model = %+v", model)
	}
	outcome, err := source.TrainLearnedModel(
		context.Background(),
		adminui.LearnedModelLinearLambdaRank,
	)
	if err != nil || !outcome.Promoted || outcome.HeldOutNDCGGain != 0.04 ||
		outcome.Confidence != 0.95 || outcome.TrainQueryCount != 6 ||
		outcome.DevelopmentQueryCount != 2 || outcome.TestQueryCount != 3 ||
		len(outcome.Reasons) != 1 {
		t.Fatalf("outcome = %+v, err = %v", outcome, err)
	}
	runner.outcome.Decision.Reasons[0] = "changed"
	if outcome.Reasons[0] != "accepted" {
		t.Fatal("training outcome reasons were not cloned")
	}
	rolledBack, err := source.RollbackLearnedModel(context.Background())
	if err != nil || !rolledBack {
		t.Fatalf("rollback = %v, %v", rolledBack, err)
	}
}

func TestRankingConsoleLearnedModelFailures(t *testing.T) {
	base := newRankingConsole(testRankingHolder(t), fakeRanker{}, fakeCurated{}).(adminui.LearnedRankingSource)
	if model := base.LearnedModel(context.Background()); model != (adminui.LearnedModelView{}) {
		t.Fatalf("missing model = %+v", model)
	}
	if _, err := base.TrainLearnedModel(
		context.Background(),
		adminui.LearnedModelLinearLambdaRank,
	); err == nil {
		t.Fatal("missing trainer was accepted")
	}
	if _, err := base.RollbackLearnedModel(context.Background()); err == nil {
		t.Fatal("missing catalog was accepted")
	}

	catalog := &rankingModelCatalogFixture{err: errors.New("disk")}
	source := newRankingConsole(
		testRankingHolder(t),
		fakeRanker{},
		fakeCurated{},
		rankingConsoleLearning{
			trainer: rankingTrainingRunnerFixture{err: errors.New("fit")},
			models:  catalog,
		},
	).(adminui.LearnedRankingSource)
	if _, err := source.TrainLearnedModel(
		context.Background(),
		adminui.LearnedModelHistogramLambdaMART,
	); err == nil {
		t.Fatal("training failure did not surface")
	}
	if _, err := source.RollbackLearnedModel(context.Background()); err == nil {
		t.Fatal("rollback failure did not surface")
	}
}
