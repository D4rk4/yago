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
	index    searchindex.SearchIndex
	weights  func() searchindex.RankingWeights
	hostRank func() hostrank.AuthorityTable
	ranker   *learnedrank.Ranker
	deny     denylistSnapshotter
}

type searchExplainRequest struct {
	Query   string                      `json:"query"`
	Weights *searchindex.RankingWeights `json:"weights"`
}

type searchExplainResult struct {
	URL                  string                         `json:"url"`
	Score                float64                        `json:"score"`
	Quality              float64                        `json:"quality"`
	QualityKnown         bool                           `json:"qualityKnown"`
	SpamRisk             float64                        `json:"spamRisk"`
	FunctionWordFraction float64                        `json:"functionWordFraction"`
	SymbolFraction       float64                        `json:"symbolFraction"`
	AlphabeticFraction   float64                        `json:"alphabeticFraction"`
	UniqueTokenFraction  float64                        `json:"uniqueTokenFraction"`
	Proximity            float64                        `json:"proximity"`
	FieldScores          map[string]float64             `json:"fieldScores,omitempty"`
	Explanation          string                         `json:"explanation,omitempty"`
	Learned              *learnedrank.ResultExplanation `json:"learned,omitempty"`
}

type searchExplainResponse struct {
	Query         string                     `json:"query"`
	Weights       searchindex.RankingWeights `json:"weights"`
	Results       []searchExplainResult      `json:"results"`
	ModelRevision string                     `json:"modelRevision,omitempty"`
	ModelKind     learnedrank.ModelKind      `json:"modelKind,omitempty"`
}

func newSearchExplainEndpoint(
	index searchindex.SearchIndex,
	weights func() searchindex.RankingWeights,
	hostRank func() hostrank.AuthorityTable,
	ranker *learnedrank.Ranker,
	deny denylistSnapshotter,
) http.Handler {
	return searchExplainEndpoint{
		index: index, weights: weights, hostRank: hostRank, ranker: ranker, deny: deny,
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
	if e.index == nil {
		return searchExplainResponse{}, http.StatusServiceUnavailable, errors.New(
			"search index unavailable",
		)
	}

	defaultWeights := searchindex.DefaultRankingWeights()
	if e.weights != nil {
		defaultWeights = e.weights()
	}
	query, weights, err := searchExplainParameters(r, defaultWeights)
	if err != nil {
		return searchExplainResponse{}, http.StatusBadRequest, err
	}
	outcome, err := e.rankingOutcome(r.Context(), query, weights)
	if err != nil {
		return searchExplainResponse{}, http.StatusInternalServerError, err
	}

	return searchExplainResponse{
		Query:         query,
		Weights:       weights,
		Results:       searchExplainResults(outcome),
		ModelRevision: outcome.SnapshotRevision,
		ModelKind:     outcome.ModelKind,
	}, http.StatusOK, nil
}

func (e searchExplainEndpoint) rankingOutcome(
	ctx context.Context,
	query string,
	weights searchindex.RankingWeights,
) (learnedrank.Outcome, error) {
	weightProvider := func() searchindex.RankingWeights { return weights }
	searcher := searchcore.NewLexicalEvidenceSearcherWithWeights(
		searchcore.NewPseudoRelevanceSearcher(newLocalRankingSearcher(
			e.index,
			weightProvider,
			e.hostRank,
		)),
		lexicalRankingWeights(weightProvider),
	)
	servingRequest := searchcore.RequestWithParsedQuery(searchcore.Request{
		Query:   query,
		Source:  searchcore.SourceLocal,
		Limit:   searchExplainMaxResults,
		Explain: true,
	})
	candidateRequest := servingRequest
	if e.ranker != nil {
		if _, active := e.ranker.ActiveSnapshot(); active {
			candidateRequest.Limit = max(
				candidateRequest.Limit,
				e.ranker.CandidateWindow(),
			)
		}
	}
	resultSet, err := searcher.Search(
		ctx,
		candidateRequest,
	)
	if err != nil {
		return learnedrank.Outcome{}, fmt.Errorf(
			"search failed: %w",
			err,
		)
	}

	resultSet.Request = servingRequest
	outcome := learnedrank.Outcome{Results: resultSet.Results}
	if e.ranker != nil {
		outcome, err = e.ranker.Rerank(servingRequest, resultSet.Results)
		if err != nil {
			return learnedrank.Outcome{}, fmt.Errorf(
				"learned ranking failed: %w",
				err,
			)
		}
	}
	filtered := filterDenylistedResponse(searchcore.Response{
		Request: resultSet.Request, TotalResults: resultSet.TotalResults, Results: outcome.Results,
	}, e.deny)
	outcome.Results = searchcore.DiversifyResults(filtered.Results, resultSet.Request)
	searchcore.OrderByDateWhenRequested(outcome.Results, resultSet.Request)
	outcome.Results = outcome.Results[:min(len(outcome.Results), searchExplainMaxResults)]

	return outcome, nil
}

func searchExplainResults(outcome learnedrank.Outcome) []searchExplainResult {
	explanations := learnedExplanationsByIdentity(outcome.Explanations)
	results := make([]searchExplainResult, 0, len(outcome.Results))
	for index, result := range outcome.Results {
		learned := explanations[learnedResultIdentity(result)]
		if learned != nil {
			learned.FinalRank = index + 1
		}
		results = append(results, searchExplainResult{
			URL:                  result.URL,
			Score:                result.Score,
			Quality:              result.Quality,
			QualityKnown:         result.QualityKnown,
			SpamRisk:             result.SpamRisk,
			FunctionWordFraction: result.FunctionWordFraction,
			SymbolFraction:       result.SymbolFraction,
			AlphabeticFraction:   result.AlphabeticFraction,
			UniqueTokenFraction:  result.UniqueTokenFraction,
			Proximity:            result.Proximity,
			FieldScores:          result.FieldScores,
			Explanation:          result.Explanation,
			Learned:              learned,
		})
	}

	return results
}

func searchExplainParameters(
	r *http.Request,
	defaultWeights searchindex.RankingWeights,
) (string, searchindex.RankingWeights, error) {
	var body searchExplainRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return "", searchindex.RankingWeights{}, fmt.Errorf("decode request: %w", err)
	}
	query := strings.TrimSpace(body.Query)
	if query == "" {
		return "", searchindex.RankingWeights{}, errors.New("query is required")
	}
	weights := defaultWeights
	if body.Weights != nil {
		weights = *body.Weights
	}
	if err := weights.Validate(); err != nil {
		return "", searchindex.RankingWeights{}, fmt.Errorf("invalid weights: %w", err)
	}

	return query, weights, nil
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
