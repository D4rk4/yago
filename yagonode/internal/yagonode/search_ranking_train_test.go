package yagonode

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/learnedrank"
	"github.com/D4rk4/yago/yagonode/internal/rankfit"
	"github.com/D4rk4/yago/yagonode/internal/rankingmodel"
	"github.com/D4rk4/yago/yagonode/internal/rankingtrain"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searcheval"
)

type rankingJudgmentsFixture struct {
	judgments []searcheval.Judgment
	err       error
}

func (fixture rankingJudgmentsFixture) trainingJudgments(
	context.Context,
) ([]searcheval.Judgment, error) {
	return fixture.judgments, fixture.err
}

type rankingCatalogActivatorFixture struct {
	snapshot         learnedrank.Snapshot
	activeSnapshot   []byte
	err              error
	incumbentChanged bool
}

func (fixture *rankingCatalogActivatorFixture) Snapshot() rankingmodel.CatalogSnapshot {
	return rankingmodel.CatalogSnapshot{
		ActiveSnapshot: append([]byte(nil), fixture.activeSnapshot...),
	}
}

func (fixture *rankingCatalogActivatorFixture) ActivateIfCurrent(
	_ context.Context,
	incumbent []byte,
	snapshot learnedrank.Snapshot,
) (bool, error) {
	if fixture.incumbentChanged || string(incumbent) != string(fixture.activeSnapshot) {
		return false, nil
	}
	fixture.snapshot = snapshot

	return true, fixture.err
}

type rankingProposalFixture struct {
	snapshot learnedrank.Snapshot
	counts   rankingtrain.DatasetCounts
	training rankingtrain.TrainingReport
	dev      rankingtrain.EvaluationComparison
	test     rankingtrain.EvaluationComparison
	decision searcheval.PromotionDecision
}

func (fixture rankingProposalFixture) Snapshot() learnedrank.Snapshot {
	return fixture.snapshot
}

func (fixture rankingProposalFixture) Counts() rankingtrain.DatasetCounts {
	return fixture.counts
}

func (fixture rankingProposalFixture) TrainingReport() rankingtrain.TrainingReport {
	return fixture.training
}

func (fixture rankingProposalFixture) DevelopmentEvaluation() rankingtrain.EvaluationComparison {
	return fixture.dev
}

func (fixture rankingProposalFixture) TestEvaluation() rankingtrain.EvaluationComparison {
	return fixture.test
}

func (fixture rankingProposalFixture) Decision() searcheval.PromotionDecision {
	return fixture.decision
}

func TestRankingModelTrainerCoversValidationAndProposalFailure(t *testing.T) {
	if _, err := (*rankingModelTrainer)(nil).Train(
		context.Background(),
		"v1",
		rankingtrain.FamilyLinearLambdaRank,
	); err == nil {
		t.Fatal("nil trainer was accepted")
	}

	candidates := stubSearcher{}
	if _, err := newRankingModelTrainer(candidates, nil, nil, nil).Train(
		context.Background(),
		"v1",
		rankingtrain.FamilyLinearLambdaRank,
	); err == nil {
		t.Fatal("missing stores were accepted")
	}
	trainer := newRankingModelTrainer(
		candidates,
		rankingJudgmentsFixture{err: errors.New("labels")},
		&rankingCatalogActivatorFixture{},
		time.Now,
	)
	if _, err := trainer.Train(
		context.Background(),
		"v1",
		rankingtrain.ModelFamily("future"),
	); err == nil {
		t.Fatal("unknown model family was accepted")
	}
	if _, err := trainer.Train(
		context.Background(),
		"v1",
		rankingtrain.FamilyLinearLambdaRank,
	); err == nil {
		t.Fatal("judgment failure did not surface")
	}

	trainer = newRankingModelTrainer(
		candidates,
		rankingJudgmentsFixture{judgments: []searcheval.Judgment{{
			Query: "query", Relevant: map[string]int{"https://example.test/": 3},
		}}},
		&rankingCatalogActivatorFixture{},
		func() time.Time { return time.Date(2026, 7, 11, 12, 13, 14, 15, time.FixedZone("x", 3600)) },
	)
	if _, err := trainer.Train(
		context.Background(),
		"",
		rankingtrain.FamilyLinearLambdaRank,
	); err == nil || !strings.Contains(err.Error(), "proposal") {
		t.Fatalf("proposal error = %v", err)
	}
	trainer.catalog = &rankingCatalogActivatorFixture{activeSnapshot: []byte("{")}
	if _, err := trainer.Train(
		context.Background(),
		"v1",
		rankingtrain.FamilyLinearLambdaRank,
	); err == nil || !strings.Contains(err.Error(), "active ranking model") {
		t.Fatalf("active model error = %v", err)
	}
	if generatedRankingRevision(
		time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC),
		rankingtrain.FamilyHistogramLambdaMART,
	) != "yagorank-20260711T000000.000000000Z-histogram" {
		t.Fatal("histogram revision is not deterministic")
	}
}

func TestRankingModelTrainerPromotesOnlyAcceptedProposal(t *testing.T) {
	proposal := rankingPromotionProposalFixture(t)
	activeSnapshot, err := rankingSnapshotFixture(t, "active-v1").MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	sawIncumbent := false
	build := func(
		_ context.Context,
		_ searchcore.Searcher,
		_ []searcheval.Judgment,
		config rankingtrain.Config,
	) (rankingProposal, error) {
		sawIncumbent = config.Incumbent != nil && config.Incumbent.Revision() == "active-v1"
		return proposal, nil
	}
	catalog := &rankingCatalogActivatorFixture{activeSnapshot: activeSnapshot}
	trainer := newRankingModelTrainer(
		stubSearcher{},
		rankingJudgmentsFixture{},
		catalog,
		time.Now,
	)
	trainer.build = build
	outcome, err := trainer.Train(
		context.Background(),
		" candidate-v1 ",
		rankingtrain.FamilyLinearLambdaRank,
	)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Promoted ||
		catalog.snapshot.Revision() != "" ||
		outcome.Revision != "candidate-v1" ||
		!sawIncumbent {
		t.Fatalf("rejected outcome = %+v, activation = %q", outcome, catalog.snapshot.Revision())
	}

	proposal.decision.Promote = true
	trainer.build = build
	catalog.incumbentChanged = true
	if _, err := trainer.Train(
		context.Background(),
		"candidate-v1",
		rankingtrain.FamilyLinearLambdaRank,
	); !errors.Is(err, errRankingModelIncumbentChanged) {
		t.Fatalf("incumbent change error = %v", err)
	}
	catalog.incumbentChanged = false
	catalog.err = errors.New("disk")
	if _, err := trainer.Train(
		context.Background(),
		"candidate-v1",
		rankingtrain.FamilyLinearLambdaRank,
	); err == nil {
		t.Fatal("activation failure did not surface")
	}
	catalog.err = nil
	outcome, err = trainer.Train(
		context.Background(),
		"candidate-v1",
		rankingtrain.FamilyLinearLambdaRank,
	)
	if err != nil || !outcome.Promoted || catalog.snapshot.Revision() != "candidate-v1" {
		t.Fatalf(
			"promoted outcome = %+v, activation = %q, err = %v",
			outcome,
			catalog.snapshot.Revision(),
			err,
		)
	}

	assertRankingProposalFactoryFailure(t, trainer)
}

func assertRankingProposalFactoryFailure(t *testing.T, trainer *rankingModelTrainer) {
	t.Helper()
	trainer.build = func(
		context.Context,
		searchcore.Searcher,
		[]searcheval.Judgment,
		rankingtrain.Config,
	) (rankingProposal, error) {
		return nil, errors.New("fit")
	}
	if _, err := trainer.Train(
		context.Background(),
		"candidate-v2",
		rankingtrain.FamilyLinearLambdaRank,
	); err == nil {
		t.Fatal("proposal factory failure did not surface")
	}
}

func rankingPromotionProposalFixture(t *testing.T) rankingProposalFixture {
	t.Helper()

	return rankingProposalFixture{
		snapshot: rankingSnapshotFixture(t, "candidate-v1"),
		counts: rankingtrain.DatasetCounts{
			Train:       rankingtrain.PartitionCounts{Queries: 3},
			Development: rankingtrain.PartitionCounts{Queries: 2},
			Test:        rankingtrain.PartitionCounts{Queries: 2},
		},
		training: rankingtrain.TrainingReport{
			Family: rankingtrain.FamilyLinearLambdaRank, PreferencePairs: 4, Iterations: 2,
		},
		decision: searcheval.PromotionDecision{
			Confidence: searcheval.BootstrapConfidence{
				ObservedRelativeGain: 0.04, Confidence: 0.95, Samples: 2000,
			},
		},
	}
}

func TestRankingTrainingCandidateSourceAvailability(t *testing.T) {
	if newRankingTrainingCandidateSource(nil, nil, nil) != nil {
		t.Fatal("nil index produced a candidate source")
	}
	if newRankingTrainingCandidateSource(stubSearchIndex{}, nil, nil) == nil {
		t.Fatal("search index did not produce a candidate source")
	}
}

type rankingTrainingRunnerFixture struct {
	outcome rankingTrainingOutcome
	err     error
}

func (fixture rankingTrainingRunnerFixture) Train(
	context.Context,
	string,
	rankingtrain.ModelFamily,
) (rankingTrainingOutcome, error) {
	return fixture.outcome, fixture.err
}

type rankingTrainingEndpointCase struct {
	name    string
	method  string
	body    string
	runner  rankingTrainingRunner
	want    int
	contain string
}

func TestSearchRankingTrainEndpointCoversHTTPContract(t *testing.T) {
	tests := append(
		rankingTrainingEndpointFailureCases(),
		rankingTrainingEndpointSuccessCase(),
	)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequestWithContext(
				t.Context(),
				test.method,
				pathSearchRankingTrain,
				strings.NewReader(test.body),
			)
			newSearchRankingTrainEndpoint(test.runner).ServeHTTP(recorder, request)
			if recorder.Code != test.want || test.contain != "" &&
				!strings.Contains(recorder.Body.String(), test.contain) {
				t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
			}
			if test.name == "method" && recorder.Header().Get("Allow") != http.MethodPost {
				t.Fatalf("Allow = %q", recorder.Header().Get("Allow"))
			}
		})
	}
}

func rankingTrainingEndpointFailureCases() []rankingTrainingEndpointCase {
	return []rankingTrainingEndpointCase{
		{
			name:   "method",
			method: http.MethodGet,
			runner: rankingTrainingRunnerFixture{},
			want:   http.StatusMethodNotAllowed,
		},
		{
			name:   "unavailable",
			method: http.MethodPost,
			body:   `{}`,
			want:   http.StatusServiceUnavailable,
		},
		{
			name:   "malformed",
			method: http.MethodPost,
			body:   `{`,
			runner: rankingTrainingRunnerFixture{},
			want:   http.StatusBadRequest,
		},
		{
			name:   "unknown",
			method: http.MethodPost,
			body:   `{"future":true}`,
			runner: rankingTrainingRunnerFixture{},
			want:   http.StatusBadRequest,
		},
		{
			name:   "trailing value",
			method: http.MethodPost,
			body:   `{} {}`,
			runner: rankingTrainingRunnerFixture{},
			want:   http.StatusBadRequest,
		},
		{
			name:   "trailing malformed",
			method: http.MethodPost,
			body:   `{} x`,
			runner: rankingTrainingRunnerFixture{},
			want:   http.StatusBadRequest,
		},
		{
			name:   "training failure",
			method: http.MethodPost,
			body:   `{"model_kind":"linear_lambdarank"}`,
			runner: rankingTrainingRunnerFixture{err: errors.New("labels")},
			want:   http.StatusBadRequest,
		},
		{
			name:   "activation failure",
			method: http.MethodPost,
			body:   `{"model_kind":"linear_lambdarank"}`,
			runner: rankingTrainingRunnerFixture{err: errRankingModelActivation},
			want:   http.StatusInternalServerError,
		},
		{
			name:   "incumbent changed",
			method: http.MethodPost,
			body:   `{"model_kind":"linear_lambdarank"}`,
			runner: rankingTrainingRunnerFixture{err: errRankingModelIncumbentChanged},
			want:   http.StatusConflict,
		},
	}
}

func rankingTrainingEndpointSuccessCase() rankingTrainingEndpointCase {
	return rankingTrainingEndpointCase{
		name:   "success",
		method: http.MethodPost,
		body:   `{"revision":"v1","model_kind":"linear_lambdarank"}`,
		runner: rankingTrainingRunnerFixture{outcome: rankingTrainingOutcome{
			Revision: "v1",
			Family:   rankingtrain.FamilyLinearLambdaRank,
			Promoted: true,
			Counts: rankingtrain.DatasetCounts{
				Train: rankingtrain.PartitionCounts{Queries: 3},
			},
			Training: rankingtrain.TrainingReport{PreferencePairs: 4},
			Development: rankingtrain.EvaluationComparison{
				Baseline:  searcheval.EvaluationReport{Metrics: rankingMetricFixture(0.4)},
				Incumbent: evaluationReportPointer(rankingMetricFixture(0.45)),
				Candidate: searcheval.EvaluationReport{Metrics: rankingMetricFixture(0.5)},
			},
			Test: rankingtrain.EvaluationComparison{
				Baseline:  searcheval.EvaluationReport{Metrics: rankingMetricFixture(0.3)},
				Candidate: searcheval.EvaluationReport{Metrics: rankingMetricFixture(0.6)},
			},
			Decision: searcheval.PromotionDecision{
				Promote: true,
				Confidence: searcheval.BootstrapConfidence{
					ObservedRelativeGain: 0.03, LowerRelativeGain: 0.01,
					UpperRelativeGain: 0.05, Confidence: 0.95, Samples: 2000,
					QueryClusters: 24,
				},
				IncumbentConfidence: &searcheval.BootstrapConfidence{
					ObservedRelativeGain: 0.025,
					LowerRelativeGain:    0.005,
					UpperRelativeGain:    0.04,
				},
				Reasons: []string{},
			},
		}},
		want: http.StatusOK, contain: `"compared_with_incumbent":true`,
	}
}

func evaluationReportPointer(metrics searcheval.MetricSet) *searcheval.EvaluationReport {
	return &searcheval.EvaluationReport{Metrics: metrics}
}

func rankingMetricFixture(ndcg float64) searcheval.MetricSet {
	return searcheval.MetricSet{
		Queries: 2, RecallAt100: 0.1, RecallAt200: 0.2, NDCGAt10: ndcg,
		ERRAt10: 0.3, NavigationalMRR: 0.4, AlphaNDCGAt10: 0.5,
		IntentCoverageAt10: 0.6, DuplicateClusterRateAt10: 0.1,
		UniqueRegistrableDomainCoverage: 0.8, UnsafeErrors: 1, SpamErrors: 2,
		PeerBytes: 3, PeerTimeouts: 4, CPULatencyP50: time.Millisecond,
		CPULatencyP95: 2 * time.Millisecond,
	}
}

func rankingSnapshotFixture(t *testing.T, revision string) learnedrank.Snapshot {
	t.Helper()
	model, err := rankfit.NewLinearLambdaRankModel(
		learnedrank.FeatureDefinitions(),
		make([]float64, len(learnedrank.FeatureDefinitions())),
	)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := learnedrank.NewLinearSnapshot(revision, model)
	if err != nil {
		t.Fatal(err)
	}

	return snapshot
}
