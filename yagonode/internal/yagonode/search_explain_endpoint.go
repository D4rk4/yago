package yagonode

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

const (
	pathSearchExplain       = "/api/admin/v1/search/explain"
	searchExplainMaxResults = 10
)

type searchExplainEndpoint struct {
	index searchindex.SearchIndex
}

type searchExplainRequest struct {
	Query   string                      `json:"query"`
	Weights *searchindex.RankingWeights `json:"weights"`
}

type searchExplainResult struct {
	URL         string             `json:"url"`
	Score       float64            `json:"score"`
	Quality     float64            `json:"quality"`
	Proximity   float64            `json:"proximity"`
	FieldScores map[string]float64 `json:"fieldScores,omitempty"`
	Explanation string             `json:"explanation,omitempty"`
}

type searchExplainResponse struct {
	Query   string                     `json:"query"`
	Weights searchindex.RankingWeights `json:"weights"`
	Results []searchExplainResult      `json:"results"`
}

func newSearchExplainEndpoint(index searchindex.SearchIndex) http.Handler {
	return searchExplainEndpoint{index: index}
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

	var body searchExplainRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return searchExplainResponse{}, http.StatusBadRequest, fmt.Errorf("decode request: %w", err)
	}
	query := strings.TrimSpace(body.Query)
	if query == "" {
		return searchExplainResponse{}, http.StatusBadRequest, errors.New("query is required")
	}
	weights := searchindex.DefaultRankingWeights()
	if body.Weights != nil {
		weights = *body.Weights
	}
	if err := weights.Validate(); err != nil {
		return searchExplainResponse{}, http.StatusBadRequest, fmt.Errorf(
			"invalid weights: %w",
			err,
		)
	}

	resultSet, err := e.index.Search(r.Context(), searchindex.SearchRequest{
		Query:      query,
		MaxResults: searchExplainMaxResults,
		Weights:    weights,
		Explain:    true,
	})
	if err != nil {
		return searchExplainResponse{}, http.StatusInternalServerError, fmt.Errorf(
			"search failed: %w",
			err,
		)
	}

	results := make([]searchExplainResult, 0, len(resultSet.Results))
	for _, result := range resultSet.Results {
		results = append(results, searchExplainResult{
			URL:         result.URL,
			Score:       result.Score,
			Quality:     result.Quality,
			Proximity:   result.Proximity,
			FieldScores: result.FieldScores,
			Explanation: result.Explanation,
		})
	}

	return searchExplainResponse{
		Query:   query,
		Weights: weights,
		Results: results,
	}, http.StatusOK, nil
}
