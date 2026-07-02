package yagonode

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/searchindex"
)

const pathIndexStats = "/api/admin/v1/index/stats"

type indexStatsEndpoint struct {
	index searchindex.SearchIndex
	now   func() time.Time
}

type indexStatsResponse struct {
	GeneratedAt string `json:"generatedAt"`
	Available   bool   `json:"available"`
	Backend     string `json:"backend,omitempty"`
	Documents   int    `json:"documents,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

func newIndexStatsEndpoint(index searchindex.SearchIndex) http.Handler {
	return indexStatsEndpoint{
		index: index,
		now:   time.Now,
	}
}

func (e indexStatsEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
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

func (e indexStatsEndpoint) response(
	r *http.Request,
) (indexStatsResponse, int, error) {
	generatedAt := e.now().UTC().Format(time.RFC3339)
	if e.index == nil {
		return indexStatsResponse{
			GeneratedAt: generatedAt,
			Available:   false,
		}, http.StatusServiceUnavailable, nil
	}

	stats, err := e.index.Stats(r.Context())
	if err != nil {
		return indexStatsResponse{}, http.StatusInternalServerError, fmt.Errorf(
			"search index stats failed: %w",
			err,
		)
	}

	return indexStatsResponse{
		GeneratedAt: generatedAt,
		Available:   true,
		Backend:     stats.Backend,
		Documents:   stats.Documents,
		UpdatedAt:   formattedIndexStatsTime(stats.UpdatedAt),
	}, http.StatusOK, nil
}

func formattedIndexStatsTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}

	return value.UTC().Format(time.RFC3339)
}
