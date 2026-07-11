package rankingtrain

import (
	"time"

	"github.com/D4rk4/yago/yagonode/internal/learnedrank"
	"github.com/D4rk4/yago/yagonode/internal/searcheval"
)

const (
	MaximumJudgments          = 10000
	MaximumCandidatesPerQuery = 200
	MaximumCandidatePool      = 200000
	MaximumModelExamples      = 100000
	MaximumPreferencePairs    = 1000000
	ModelCandidateWindow      = 100
	MaximumTrainingQueries    = 1000
)

type ModelFamily string

const (
	FamilyLinearLambdaRank    ModelFamily = "linear_lambdarank"
	FamilyHistogramLambdaMART ModelFamily = "histogram_lambdamart"
)

type Config struct {
	Revision         string
	Family           ModelFamily
	Incumbent        *learnedrank.Snapshot
	Split            searcheval.HoldoutSplitConfig
	PromotionPolicy  searcheval.PromotionPolicy
	MeasurementClock func() time.Time
}

func DefaultConfig(revision string, family ModelFamily) Config {
	return Config{
		Revision:         revision,
		Family:           family,
		Split:            searcheval.DefaultHoldoutSplitConfig(),
		PromotionPolicy:  searcheval.DefaultPromotionPolicy(),
		MeasurementClock: time.Now,
	}
}

type PartitionCounts struct {
	Queries       int `json:"queries"`
	Candidates    int `json:"candidates"`
	ModelExamples int `json:"model_examples"`
}

type DatasetCounts struct {
	Train       PartitionCounts `json:"train"`
	Development PartitionCounts `json:"development"`
	Test        PartitionCounts `json:"test"`
}

type TrainingReport struct {
	Family          ModelFamily `json:"model_kind"`
	PreferencePairs int         `json:"preference_pairs"`
	Iterations      int         `json:"iterations"`
	Trees           int         `json:"trees"`
}

type EvaluationComparison struct {
	Baseline  searcheval.EvaluationReport
	Incumbent *searcheval.EvaluationReport
	Candidate searcheval.EvaluationReport
}

type Proposal struct {
	snapshot    learnedrank.Snapshot
	counts      DatasetCounts
	training    TrainingReport
	development EvaluationComparison
	test        EvaluationComparison
	decision    searcheval.PromotionDecision
}

func (p Proposal) Snapshot() learnedrank.Snapshot {
	return p.snapshot
}

func (p Proposal) Counts() DatasetCounts {
	return p.counts
}

func (p Proposal) TrainingReport() TrainingReport {
	return p.training
}

func (p Proposal) DevelopmentEvaluation() EvaluationComparison {
	return cloneEvaluationComparison(p.development)
}

func (p Proposal) TestEvaluation() EvaluationComparison {
	return cloneEvaluationComparison(p.test)
}

func (p Proposal) Decision() searcheval.PromotionDecision {
	return clonePromotionDecision(p.decision)
}

func cloneEvaluationComparison(comparison EvaluationComparison) EvaluationComparison {
	cloned := EvaluationComparison{
		Baseline:  cloneEvaluationReport(comparison.Baseline),
		Candidate: cloneEvaluationReport(comparison.Candidate),
	}
	if comparison.Incumbent != nil {
		incumbent := cloneEvaluationReport(*comparison.Incumbent)
		cloned.Incumbent = &incumbent
	}

	return cloned
}

func cloneEvaluationReport(report searcheval.EvaluationReport) searcheval.EvaluationReport {
	cloned := searcheval.EvaluationReport{
		Metrics: report.Metrics,
		Slices:  make(map[string]searcheval.MetricSet, len(report.Slices)),
		Queries: make([]searcheval.QueryMetrics, len(report.Queries)),
	}
	for name, metrics := range report.Slices {
		cloned.Slices[name] = metrics
	}
	for index, query := range report.Queries {
		cloned.Queries[index] = query
		cloned.Queries[index].SliceNames = append([]string(nil), query.SliceNames...)
	}

	return cloned
}
