package yagonode

import (
	"context"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/searcheval"
)

type rankingTrainingReadiness struct {
	Available                   bool
	Ready                       bool
	Judgments                   int
	QueryClusters               int
	HeldoutQueryClusters        int
	MinimumHeldoutQueryClusters int
}

type rankingTrainingReadinessSource interface {
	TrainingReadiness(context.Context) rankingTrainingReadiness
}

func (t rankingTuner) TrainingReadiness(ctx context.Context) rankingTrainingReadiness {
	readiness := rankingTrainingReadiness{
		MinimumHeldoutQueryClusters: searcheval.DefaultPromotionPolicy().MinimumHeldoutQueryClusters,
	}
	if t.curated == nil {
		return readiness
	}
	judgments, err := t.trainingJudgments(ctx)
	if err != nil {
		return readiness
	}
	canonical := make([]searcheval.CanonicalJudgment, 0, len(judgments))
	for _, judgment := range judgments {
		canonical = append(canonical, searcheval.CanonicalJudgment{
			Query:        judgment.Query,
			QueryCluster: judgment.QueryCluster,
			ObservedAt:   judgment.ObservedAt,
		})
	}
	split, err := searcheval.SplitHeldoutJudgments(
		canonical,
		searcheval.DefaultHoldoutSplitConfig(),
	)
	if err != nil {
		return readiness
	}
	readiness.Available = true
	readiness.Judgments = len(judgments)
	readiness.QueryClusters = independentQueryClusters(canonical)
	readiness.HeldoutQueryClusters = independentQueryClusters(append(
		append([]searcheval.CanonicalJudgment(nil), split.Test...),
		split.Chronological...,
	))
	readiness.Ready = len(split.Train) > 0 && len(split.Development) > 0 &&
		len(split.Test)+len(split.Chronological) > 0 &&
		readiness.HeldoutQueryClusters >= readiness.MinimumHeldoutQueryClusters

	return readiness
}

func independentQueryClusters(judgments []searcheval.CanonicalJudgment) int {
	clusters := make(map[string]struct{}, len(judgments))
	for _, judgment := range judgments {
		cluster := strings.Join(strings.Fields(strings.ToLower(judgment.QueryCluster)), " ")
		if cluster == "" {
			cluster = strings.Join(strings.Fields(strings.ToLower(judgment.Query)), " ")
		}
		if cluster != "" {
			clusters[cluster] = struct{}{}
		}
	}

	return len(clusters)
}
