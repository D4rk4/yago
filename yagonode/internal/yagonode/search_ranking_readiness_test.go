package yagonode

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/judgments"
	"github.com/D4rk4/yago/yagonode/internal/rankingtrain"
	"github.com/D4rk4/yago/yagonode/internal/searcheval"
)

type readinessCurated struct {
	judgments []judgments.Judgment
	err       error
}

type readinessTrainingRunner struct {
	calls int
}

func (runner *readinessTrainingRunner) Train(
	context.Context,
	string,
	rankingtrain.ModelFamily,
) (rankingTrainingOutcome, error) {
	runner.calls++

	return rankingTrainingOutcome{}, nil
}

func (fixture readinessCurated) List(context.Context) ([]judgments.Judgment, error) {
	return append([]judgments.Judgment(nil), fixture.judgments...), fixture.err
}

func TestRankingTrainingReadinessUsesActualHeldoutPolicy(t *testing.T) {
	curated := readinessCurated{judgments: rankingReadinessJudgments(1000)}
	readiness := (rankingTuner{curated: curated}).TrainingReadiness(t.Context())
	if !readiness.Available || !readiness.Ready || readiness.Judgments != 1000 ||
		readiness.QueryClusters != 1000 ||
		readiness.HeldoutQueryClusters < readiness.MinimumHeldoutQueryClusters ||
		readiness.MinimumHeldoutQueryClusters != 20 {
		t.Fatalf("readiness = %+v", readiness)
	}
}

func TestRankingTrainingReadinessRejectsSparseOrUnavailableEvidence(t *testing.T) {
	if readiness := (rankingTuner{}).TrainingReadiness(t.Context()); readiness.Available {
		t.Fatalf("nil readiness = %+v", readiness)
	}
	curated := readinessCurated{judgments: rankingReadinessJudgments(1)}
	readiness := (rankingTuner{curated: curated}).TrainingReadiness(t.Context())
	if !readiness.Available || readiness.Ready || readiness.Judgments != 1 {
		t.Fatalf("sparse readiness = %+v", readiness)
	}
	for _, fixture := range []readinessCurated{
		{err: errors.New("read failed")},
		{judgments: []judgments.Judgment{{Grades: map[string]int{"https://example.com/": 1}}}},
	} {
		if readiness := (rankingTuner{curated: fixture}).TrainingReadiness(
			t.Context(),
		); readiness.Available {
			t.Fatalf("invalid readiness = %+v", readiness)
		}
	}
}

func TestIndependentQueryClustersFallsBackToQueryAndSkipsEmpty(t *testing.T) {
	if got := independentQueryClusters([]searcheval.CanonicalJudgment{
		{Query: " Query A "},
		{},
	}); got != 1 {
		t.Fatalf("clusters = %d, want 1", got)
	}
}

func TestRankingConsoleProfileReportsTrainingReadiness(t *testing.T) {
	curated := readinessCurated{judgments: rankingReadinessJudgments(1000)}
	profile := newRankingConsole(
		testRankingHolder(t),
		rankingTuner{curated: curated},
		curated,
	).Profile(t.Context())
	if !profile.TrainingReadinessAvailable || !profile.ModelTrainingReady ||
		profile.TrainingJudgmentCount != 1000 ||
		profile.HeldoutQueryClusterCount < profile.MinimumHeldoutQueryClusters {
		t.Fatalf("profile readiness = %+v", profile)
	}
}

func TestRankingConsoleRejectsTrainingBeforeHeldoutEvidenceIsReady(t *testing.T) {
	curated := readinessCurated{judgments: rankingReadinessJudgments(1)}
	runner := &readinessTrainingRunner{}
	source := newRankingConsole(
		testRankingHolder(t),
		rankingTuner{curated: curated},
		curated,
		rankingConsoleLearning{trainer: runner},
	).(adminui.LearnedRankingSource)
	if _, err := source.TrainLearnedModel(
		t.Context(),
		adminui.LearnedModelLinearLambdaRank,
	); err == nil {
		t.Fatal("sparse evidence was accepted for model training")
	}
	if runner.calls != 0 {
		t.Fatalf("training runner called %d times", runner.calls)
	}
}

func rankingReadinessJudgments(total int) []judgments.Judgment {
	out := make([]judgments.Judgment, total)
	for index := range out {
		out[index] = judgments.Judgment{
			Query:        fmt.Sprintf("query-%04d", index),
			QueryCluster: fmt.Sprintf("cluster-%04d", index),
			Grades:       map[string]int{"https://example.com/": 1},
		}
	}

	return out
}
