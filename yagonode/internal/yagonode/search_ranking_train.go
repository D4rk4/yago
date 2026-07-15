package yagonode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/learnedrank"
	"github.com/D4rk4/yago/yagonode/internal/rankingmodel"
	"github.com/D4rk4/yago/yagonode/internal/rankingtrain"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searcheval"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

const (
	pathSearchRankingTrain     = "/api/admin/v1/search/ranking/model/train"
	maximumRankingTrainingBody = 4096
	rankingRevisionTimeFormat  = "20060102T150405.000000000Z"
)

var (
	errRankingModelActivation       = errors.New("ranking model activation failed")
	errRankingModelIncumbentChanged = errors.New("ranking model incumbent changed")
)

type rankingTrainingJudgments interface {
	trainingJudgments(context.Context) ([]searcheval.Judgment, error)
}

type rankingTrainingCatalog interface {
	Snapshot() rankingmodel.CatalogSnapshot
	ActivateIfCurrent(context.Context, []byte, learnedrank.Snapshot) (bool, error)
}

type rankingProposal interface {
	Snapshot() learnedrank.Snapshot
	Counts() rankingtrain.DatasetCounts
	TrainingReport() rankingtrain.TrainingReport
	DevelopmentEvaluation() rankingtrain.EvaluationComparison
	TestEvaluation() rankingtrain.EvaluationComparison
	Decision() searcheval.PromotionDecision
}

type rankingProposalFactory func(
	context.Context,
	searchcore.Searcher,
	[]searcheval.Judgment,
	rankingtrain.Config,
) (rankingProposal, error)

type rankingTrainingOutcome struct {
	Revision    string
	Family      rankingtrain.ModelFamily
	Counts      rankingtrain.DatasetCounts
	Training    rankingtrain.TrainingReport
	Development rankingtrain.EvaluationComparison
	Test        rankingtrain.EvaluationComparison
	Decision    searcheval.PromotionDecision
	Promoted    bool
}

type rankingModelTrainer struct {
	candidates searchcore.Searcher
	judgments  rankingTrainingJudgments
	catalog    rankingTrainingCatalog
	clock      func() time.Time
	build      rankingProposalFactory
	lock       sync.Mutex
}

func newRankingModelTrainer(
	candidates searchcore.Searcher,
	judgments rankingTrainingJudgments,
	catalog rankingTrainingCatalog,
	clock func() time.Time,
) *rankingModelTrainer {
	if clock == nil {
		clock = time.Now
	}

	return &rankingModelTrainer{
		candidates: candidates,
		judgments:  judgments,
		catalog:    catalog,
		clock:      clock,
		build: func(
			ctx context.Context,
			searcher searchcore.Searcher,
			judgments []searcheval.Judgment,
			config rankingtrain.Config,
		) (rankingProposal, error) {
			return rankingtrain.BuildProposal(ctx, searcher, judgments, config)
		},
	}
}

func (trainer *rankingModelTrainer) Train(
	ctx context.Context,
	revision string,
	family rankingtrain.ModelFamily,
) (rankingTrainingOutcome, error) {
	if trainer == nil || trainer.candidates == nil {
		return rankingTrainingOutcome{}, fmt.Errorf("search index unavailable for model training")
	}
	if trainer.judgments == nil || trainer.catalog == nil {
		return rankingTrainingOutcome{}, fmt.Errorf("ranking training stores are unavailable")
	}
	if family != rankingtrain.FamilyLinearLambdaRank &&
		family != rankingtrain.FamilyHistogramLambdaMART {
		return rankingTrainingOutcome{}, fmt.Errorf("model family %q is unsupported", family)
	}
	trainer.lock.Lock()
	defer trainer.lock.Unlock()
	graded, err := trainer.judgments.trainingJudgments(ctx)
	if err != nil {
		return rankingTrainingOutcome{}, fmt.Errorf("load ranking judgments: %w", err)
	}
	revision = strings.TrimSpace(revision)
	if revision == "" {
		revision = generatedRankingRevision(trainer.clock(), family)
	}
	catalogSnapshot := trainer.catalog.Snapshot()
	config := rankingtrain.DefaultConfig(revision, family)
	if len(catalogSnapshot.ActiveSnapshot) != 0 {
		incumbent, parseErr := learnedrank.ParseSnapshot(catalogSnapshot.ActiveSnapshot)
		if parseErr != nil {
			return rankingTrainingOutcome{}, fmt.Errorf("load active ranking model: %w", parseErr)
		}
		config.Incumbent = &incumbent
	}
	proposal, err := trainer.build(
		ctx,
		trainer.candidates,
		graded,
		config,
	)
	if err != nil {
		return rankingTrainingOutcome{}, fmt.Errorf("build ranking model proposal: %w", err)
	}
	decision := proposal.Decision()
	outcome := rankingTrainingOutcome{
		Revision:    proposal.Snapshot().Revision(),
		Family:      family,
		Counts:      proposal.Counts(),
		Training:    proposal.TrainingReport(),
		Development: proposal.DevelopmentEvaluation(),
		Test:        proposal.TestEvaluation(),
		Decision:    decision,
	}
	if !decision.Promote {
		return outcome, nil
	}
	activated, err := trainer.catalog.ActivateIfCurrent(
		ctx,
		catalogSnapshot.ActiveSnapshot,
		proposal.Snapshot(),
	)
	if err != nil {
		return rankingTrainingOutcome{}, fmt.Errorf("%w: %w", errRankingModelActivation, err)
	}
	if !activated {
		return rankingTrainingOutcome{}, errRankingModelIncumbentChanged
	}
	outcome.Promoted = true

	return outcome, nil
}

func generatedRankingRevision(at time.Time, family rankingtrain.ModelFamily) string {
	suffix := "linear"
	if family == rankingtrain.FamilyHistogramLambdaMART {
		suffix = "histogram"
	}

	return "yagorank-" + at.UTC().Format(rankingRevisionTimeFormat) + "-" + suffix
}

func newRankingTrainingCandidateSource(
	index searchindex.SearchIndex,
	weights func() searchindex.RankingWeights,
	authority func() hostrank.AuthorityTable,
) searchcore.Searcher {
	if index == nil {
		return nil
	}

	return searchcore.NewLexicalEvidenceSearcherWithWeights(
		searchcore.NewPseudoRelevanceSearcher(
			newLocalRankingSearcher(index, weights, authority),
		),
		lexicalRankingWeights(weights),
	)
}

type rankingTrainingRequest struct {
	Revision  string                   `json:"revision"`
	ModelKind rankingtrain.ModelFamily `json:"model_kind"`
}

type rankingMetricSummary struct {
	Queries                         int     `json:"queries"`
	RecallAt100                     float64 `json:"recall_at_100"`
	RecallAt200                     float64 `json:"recall_at_200"`
	NDCGAt10                        float64 `json:"ndcg_at_10"`
	ERRAt10                         float64 `json:"err_at_10"`
	NavigationalMRR                 float64 `json:"navigational_mrr"`
	AlphaNDCGAt10                   float64 `json:"alpha_ndcg_at_10"`
	IntentCoverageAt10              float64 `json:"intent_coverage_at_10"`
	DuplicateClusterRateAt10        float64 `json:"duplicate_cluster_rate_at_10"`
	UniqueRegistrableDomainCoverage float64 `json:"unique_domain_coverage"`
	UnsafeErrors                    int     `json:"unsafe_errors"`
	SpamErrors                      int     `json:"spam_errors"`
	UnsafeExposureAt10              float64 `json:"unsafe_exposure_at_10"`
	SpamExposureAt10                float64 `json:"spam_exposure_at_10"`
	PeerBytes                       *int64  `json:"peer_bytes"`
	PeerTimeouts                    *int    `json:"peer_timeouts"`
	RerankLatencyP50Milliseconds    float64 `json:"rerank_wall_latency_p50_ms"`
	RerankLatencyP95Milliseconds    float64 `json:"rerank_wall_latency_p95_ms"`
}

type rankingEvaluationSummary struct {
	Baseline  rankingMetricSummary  `json:"baseline"`
	Incumbent *rankingMetricSummary `json:"incumbent,omitempty"`
	Candidate rankingMetricSummary  `json:"candidate"`
}

type rankingPromotionSummary struct {
	Promote                       bool     `json:"promote"`
	ObservedRelativeGain          float64  `json:"observed_relative_gain"`
	LowerRelativeGain             float64  `json:"lower_relative_gain"`
	UpperRelativeGain             float64  `json:"upper_relative_gain"`
	Confidence                    float64  `json:"confidence"`
	Samples                       int      `json:"samples"`
	QueryClusters                 int      `json:"query_clusters"`
	ComparedWithIncumbent         bool     `json:"compared_with_incumbent"`
	IncumbentObservedRelativeGain float64  `json:"incumbent_observed_relative_gain,omitempty"`
	IncumbentLowerRelativeGain    float64  `json:"incumbent_lower_relative_gain,omitempty"`
	IncumbentUpperRelativeGain    float64  `json:"incumbent_upper_relative_gain,omitempty"`
	Reasons                       []string `json:"reasons"`
}

type rankingTrainingResponse struct {
	Revision    string                      `json:"revision"`
	ModelKind   rankingtrain.ModelFamily    `json:"model_kind"`
	Promoted    bool                        `json:"promoted"`
	Dataset     rankingtrain.DatasetCounts  `json:"dataset"`
	Training    rankingtrain.TrainingReport `json:"training"`
	Development rankingEvaluationSummary    `json:"development"`
	Test        rankingEvaluationSummary    `json:"test"`
	Promotion   rankingPromotionSummary     `json:"promotion"`
}

type rankingTrainingRunner interface {
	Train(
		context.Context,
		string,
		rankingtrain.ModelFamily,
	) (rankingTrainingOutcome, error)
}

type searchRankingTrainEndpoint struct {
	trainer rankingTrainingRunner
}

func newSearchRankingTrainEndpoint(trainer rankingTrainingRunner) http.Handler {
	return searchRankingTrainEndpoint{trainer: trainer}
}

func (endpoint searchRankingTrainEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}
	if endpoint.trainer == nil {
		http.Error(w, "ranking model trainer unavailable", http.StatusServiceUnavailable)

		return
	}
	request, err := decodeRankingTrainingRequest(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}
	outcome, err := endpoint.trainer.Train(r.Context(), request.Revision, request.ModelKind)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errRankingModelActivation) {
			status = http.StatusInternalServerError
		} else if errors.Is(err, errRankingModelIncumbentChanged) {
			status = http.StatusConflict
		}
		http.Error(w, fmt.Sprintf("train ranking model: %v", err), status)

		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rankingTrainingResponseFor(outcome))
}

func decodeRankingTrainingRequest(
	w http.ResponseWriter,
	r *http.Request,
) (rankingTrainingRequest, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maximumRankingTrainingBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request rankingTrainingRequest
	if err := decoder.Decode(&request); err != nil {
		return rankingTrainingRequest{}, fmt.Errorf("decode ranking training request: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return rankingTrainingRequest{}, fmt.Errorf(
				"ranking training request has trailing data",
			)
		}

		return rankingTrainingRequest{}, fmt.Errorf("decode ranking training request: %w", err)
	}

	return request, nil
}

func rankingTrainingResponseFor(outcome rankingTrainingOutcome) rankingTrainingResponse {
	return rankingTrainingResponse{
		Revision:    outcome.Revision,
		ModelKind:   outcome.Family,
		Promoted:    outcome.Promoted,
		Dataset:     outcome.Counts,
		Training:    outcome.Training,
		Development: rankingEvaluationSummaryFor(outcome.Development),
		Test:        rankingEvaluationSummaryFor(outcome.Test),
		Promotion:   rankingPromotionSummaryFor(outcome.Decision),
	}
}

func rankingEvaluationSummaryFor(
	comparison rankingtrain.EvaluationComparison,
) rankingEvaluationSummary {
	summary := rankingEvaluationSummary{
		Baseline:  rankingMetricSummaryFor(comparison.Baseline.Metrics),
		Candidate: rankingMetricSummaryFor(comparison.Candidate.Metrics),
	}
	if comparison.Incumbent != nil {
		incumbent := rankingMetricSummaryFor(comparison.Incumbent.Metrics)
		summary.Incumbent = &incumbent
	}

	return summary
}

func rankingMetricSummaryFor(metrics searcheval.MetricSet) rankingMetricSummary {
	summary := rankingMetricSummary{
		Queries:                         metrics.Queries,
		RecallAt100:                     metrics.RecallAt100,
		RecallAt200:                     metrics.RecallAt200,
		NDCGAt10:                        metrics.NDCGAt10,
		ERRAt10:                         metrics.ERRAt10,
		NavigationalMRR:                 metrics.NavigationalMRR,
		AlphaNDCGAt10:                   metrics.AlphaNDCGAt10,
		IntentCoverageAt10:              metrics.IntentCoverageAt10,
		DuplicateClusterRateAt10:        metrics.DuplicateClusterRateAt10,
		UniqueRegistrableDomainCoverage: metrics.UniqueRegistrableDomainCoverage,
		UnsafeErrors:                    metrics.UnsafeErrors,
		SpamErrors:                      metrics.SpamErrors,
		UnsafeExposureAt10:              metrics.UnsafeExposureAt10,
		SpamExposureAt10:                metrics.SpamExposureAt10,
		RerankLatencyP50Milliseconds:    durationMilliseconds(metrics.RerankLatencyP50),
		RerankLatencyP95Milliseconds:    durationMilliseconds(metrics.RerankLatencyP95),
	}
	if metrics.PeerResourcesMeasured {
		summary.PeerBytes = &metrics.PeerBytes
		summary.PeerTimeouts = &metrics.PeerTimeouts
	}

	return summary
}

func rankingPromotionSummaryFor(decision searcheval.PromotionDecision) rankingPromotionSummary {
	summary := rankingPromotionSummary{
		Promote:              decision.Promote,
		ObservedRelativeGain: decision.Confidence.ObservedRelativeGain,
		LowerRelativeGain:    decision.Confidence.LowerRelativeGain,
		UpperRelativeGain:    decision.Confidence.UpperRelativeGain,
		Confidence:           decision.Confidence.Confidence,
		Samples:              decision.Confidence.Samples,
		QueryClusters:        decision.Confidence.QueryClusters,
		Reasons:              append([]string(nil), decision.Reasons...),
	}
	if decision.IncumbentConfidence != nil {
		summary.ComparedWithIncumbent = true
		summary.IncumbentObservedRelativeGain = decision.IncumbentConfidence.ObservedRelativeGain
		summary.IncumbentLowerRelativeGain = decision.IncumbentConfidence.LowerRelativeGain
		summary.IncumbentUpperRelativeGain = decision.IncumbentConfidence.UpperRelativeGain
	}

	return summary
}

func durationMilliseconds(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}
