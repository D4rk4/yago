package yagonode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/learnedrank"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

const (
	pathSearchExplain       = "/api/admin/v1/search/explain"
	searchExplainMaxResults = 10
)

type searchExplainEndpoint struct {
	index     searchindex.SearchIndex
	global    searchcore.Searcher
	weights   func() searchindex.RankingWeights
	hostRank  func() hostrank.AuthorityTable
	ranker    *learnedrank.Ranker
	deny      denylistSnapshotter
	execution searchExplanationExecutionBudget
}

type searchExplainRequest struct {
	Query   string                      `json:"query"`
	Scope   searchcore.Source           `json:"scope,omitempty"`
	Weights *searchindex.RankingWeights `json:"weights"`
}

type searchExplainResult struct {
	URL                  string                         `json:"url"`
	Source               string                         `json:"source"`
	Score                float64                        `json:"score"`
	RetrievalScore       float64                        `json:"retrievalScore"`
	Quality              float64                        `json:"quality"`
	QualityKnown         bool                           `json:"qualityKnown"`
	SpamRisk             float64                        `json:"spamRisk"`
	SpamRiskKnown        bool                           `json:"spamRiskKnown"`
	FunctionWordFraction float64                        `json:"functionWordFraction"`
	FunctionWordKnown    bool                           `json:"functionWordKnown"`
	SymbolFraction       float64                        `json:"symbolFraction"`
	SymbolKnown          bool                           `json:"symbolKnown"`
	AlphabeticFraction   float64                        `json:"alphabeticFraction"`
	AlphabeticKnown      bool                           `json:"alphabeticKnown"`
	UniqueTokenFraction  float64                        `json:"uniqueTokenFraction"`
	UniqueTokenKnown     bool                           `json:"uniqueTokenKnown"`
	Proximity            float64                        `json:"proximity"`
	ProximityKnown       bool                           `json:"proximityKnown"`
	FieldScores          map[string]float64             `json:"fieldScores,omitempty"`
	Evidence             []searchExplainSignal          `json:"evidence,omitempty"`
	Fusion               []searchExplainFusion          `json:"fusion,omitempty"`
	Explanation          string                         `json:"explanation,omitempty"`
	Learned              *learnedrank.ResultExplanation `json:"learned,omitempty"`
}

type searchExplainSignal struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

type searchExplainFusion struct {
	Branch       string  `json:"branch"`
	Rank         int     `json:"rank"`
	Contribution float64 `json:"contribution"`
}

type searchExplainResponse struct {
	Query           string                      `json:"query"`
	Scope           searchcore.Source           `json:"scope"`
	Weights         searchindex.RankingWeights  `json:"weights"`
	Results         []searchExplainResult       `json:"results"`
	PartialFailures []searchcore.PartialFailure `json:"partialFailures,omitempty"`
	ModelRevision   string                      `json:"modelRevision,omitempty"`
	ModelKind       learnedrank.ModelKind       `json:"modelKind,omitempty"`
}

func newSearchExplainEndpoint(
	index searchindex.SearchIndex,
	weights func() searchindex.RankingWeights,
	hostRank func() hostrank.AuthorityTable,
	ranker *learnedrank.Ranker,
	deny denylistSnapshotter,
) *searchExplainEndpoint {
	return &searchExplainEndpoint{
		index: index, weights: weights, hostRank: hostRank, ranker: ranker, deny: deny,
		execution: newSearchExplanationExecutionBudget(),
	}
}

func (e searchExplainEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	response, status, err := e.response(r)
	if err != nil {
		http.Error(w, err.Error(), status)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}

func (e searchExplainEndpoint) response(r *http.Request) (searchExplainResponse, int, error) {
	var request searchExplainRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		return searchExplainResponse{}, http.StatusBadRequest, fmt.Errorf("decode request: %w", err)
	}

	return e.explanation(r.Context(), request)
}

func (e searchExplainEndpoint) explanation(
	ctx context.Context,
	request searchExplainRequest,
) (searchExplainResponse, int, error) {
	defaultWeights := searchindex.DefaultRankingWeights()
	if e.weights != nil {
		defaultWeights = e.weights()
	}
	query, scope, weights, err := searchExplainParameters(request, defaultWeights)
	if err != nil {
		return searchExplainResponse{}, http.StatusBadRequest, err
	}
	if scope == searchcore.SourceLocal && e.index == nil {
		return searchExplainResponse{}, http.StatusServiceUnavailable, errors.New(
			"search index unavailable",
		)
	}
	if scope == searchcore.SourceGlobal && e.global == nil {
		return searchExplainResponse{}, http.StatusServiceUnavailable, errors.New(
			"global search explanation unavailable",
		)
	}
	if scope == searchcore.SourceGlobal && request.Weights != nil {
		return searchExplainResponse{}, http.StatusBadRequest, errors.New(
			"custom weights are available only for local explanations",
		)
	}
	outcome, err := e.rankingOutcome(ctx, query, weights, scope)
	if err != nil {
		return searchExplainResponse{}, http.StatusInternalServerError, err
	}

	return searchExplainResponse{
		Query:           query,
		Scope:           scope,
		Weights:         weights,
		Results:         searchExplainResults(outcome.ranking),
		PartialFailures: humanSearchPartialFailures(outcome.partialFailures),
		ModelRevision:   outcome.ranking.SnapshotRevision,
		ModelKind:       outcome.ranking.ModelKind,
	}, http.StatusOK, nil
}

func (e searchExplainEndpoint) localRankingOutcome(
	ctx context.Context,
	query string,
	weights searchindex.RankingWeights,
) (searchExplainOutcome, error) {
	weightProvider := func() searchindex.RankingWeights { return weights }
	assembly := publicSearchAssembly{
		rankingWeights: weightProvider,
		hostRank:       e.hostRank,
		denylist:       e.deny,
	}
	local := newLocalRankingSearcher(e.index, weightProvider, e.hostRank)
	searcher := assembleExplanationEvidenceSearcher(local, nil, assembly)

	return e.searcherRankingOutcome(ctx, query, searchcore.SourceLocal, searcher)
}

func searchExplainResults(outcome learnedrank.Outcome) []searchExplainResult {
	explanations := learnedExplanationsByIdentity(outcome.Explanations)
	results := make([]searchExplainResult, 0, len(outcome.Results))
	for index, result := range outcome.Results {
		learned := explanations[learnedResultIdentity(result)]
		diagnostics := searchExplanationDiagnosticValues(result.Evidence)
		if learned != nil {
			learned.FinalRank = index + 1
		}
		results = append(results, searchExplainResult{
			URL:                  result.URL,
			Source:               searchExplainSource(result),
			Score:                result.Score,
			RetrievalScore:       searchExplainRetrievalScore(result, learned),
			Quality:              diagnostics.quality,
			QualityKnown:         diagnostics.qualityKnown,
			SpamRisk:             diagnostics.spamRisk,
			SpamRiskKnown:        diagnostics.spamRiskKnown,
			FunctionWordFraction: diagnostics.functionWordFraction,
			FunctionWordKnown:    diagnostics.functionWordKnown,
			SymbolFraction:       diagnostics.symbolFraction,
			SymbolKnown:          diagnostics.symbolKnown,
			AlphabeticFraction:   diagnostics.alphabeticFraction,
			AlphabeticKnown:      diagnostics.alphabeticKnown,
			UniqueTokenFraction:  diagnostics.uniqueTokenFraction,
			UniqueTokenKnown:     diagnostics.uniqueTokenKnown,
			Proximity:            diagnostics.proximity,
			ProximityKnown:       diagnostics.proximityKnown,
			FieldScores:          result.FieldScores,
			Evidence:             searchExplainSignals(result.Evidence),
			Fusion:               searchExplainFusionContributions(result, learned),
			Explanation:          result.Explanation,
			Learned:              learned,
		})
	}

	return results
}

func searchExplainSignals(evidence searchcore.RankingEvidence) []searchExplainSignal {
	values := evidence.Values()
	signals := make([]searchExplainSignal, 0, len(values))
	for _, value := range values {
		signals = append(signals, searchExplainSignal{
			Name: value.Signal.Name(), Value: value.Value,
		})
	}

	return signals
}

func searchExplainParameters(
	request searchExplainRequest,
	defaultWeights searchindex.RankingWeights,
) (string, searchcore.Source, searchindex.RankingWeights, error) {
	query := strings.TrimSpace(request.Query)
	if query == "" {
		return "", "", searchindex.RankingWeights{}, errors.New("query is required")
	}
	scope := request.Scope
	if scope == "" {
		scope = searchcore.SourceLocal
	}
	if scope != searchcore.SourceLocal && scope != searchcore.SourceGlobal {
		return "", "", searchindex.RankingWeights{}, errors.New(
			"scope must be local or global",
		)
	}
	weights := defaultWeights
	if request.Weights != nil {
		weights = *request.Weights
	}
	if err := weights.Validate(); err != nil {
		return "", "", searchindex.RankingWeights{}, fmt.Errorf("invalid weights: %w", err)
	}

	return query, scope, weights, nil
}

func learnedExplanationsByIdentity(
	explanations []learnedrank.ResultExplanation,
) map[string]*learnedrank.ResultExplanation {
	byIdentity := make(map[string]*learnedrank.ResultExplanation, len(explanations))
	for index := range explanations {
		explanation := explanations[index]
		byIdentity[explanation.Identity] = &explanation
	}

	return byIdentity
}

func learnedResultIdentity(result searchcore.Result) string {
	if result.URLHash != "" {
		return "hash:" + result.URLHash
	}
	if result.URL != "" {
		return "url:" + result.URL
	}
	if result.DisplayURL != "" {
		return "display_url:" + result.DisplayURL
	}
	if result.Title != "" {
		return "title:" + result.Title
	}

	return ""
}
