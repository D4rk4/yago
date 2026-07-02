package yagonode

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/searchindex"
)

const pathReady = "/ready"

type readinessEndpoint struct {
	index searchindex.SearchIndex
	now   func() time.Time
}

type readinessResponse struct {
	GeneratedAt string                   `json:"generatedAt"`
	Ready       bool                     `json:"ready"`
	Checks      []readinessCheckResponse `json:"checks"`
}

type readinessCheckResponse struct {
	Name      string `json:"name"`
	Ready     bool   `json:"ready"`
	Reason    string `json:"reason,omitempty"`
	Backend   string `json:"backend,omitempty"`
	Documents int    `json:"documents,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

func newReadinessEndpoint(index searchindex.SearchIndex) http.Handler {
	return readinessEndpoint{
		index: index,
		now:   time.Now,
	}
}

func (e readinessEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response, status := e.response(r)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}

func (e readinessEndpoint) response(r *http.Request) (readinessResponse, int) {
	check := e.searchIndexCheck(r)
	status := http.StatusOK
	if !check.Ready {
		status = http.StatusServiceUnavailable
	}

	return readinessResponse{
		GeneratedAt: e.now().UTC().Format(time.RFC3339),
		Ready:       check.Ready,
		Checks:      []readinessCheckResponse{check},
	}, status
}

func (e readinessEndpoint) searchIndexCheck(r *http.Request) readinessCheckResponse {
	if e.index == nil {
		return readinessCheckResponse{
			Name:   "search_index",
			Ready:  false,
			Reason: "unavailable",
		}
	}

	stats, err := e.index.Stats(r.Context())
	if err != nil {
		return readinessCheckResponse{
			Name:   "search_index",
			Ready:  false,
			Reason: "stats_failed",
		}
	}

	return readinessCheckResponse{
		Name:      "search_index",
		Ready:     true,
		Backend:   stats.Backend,
		Documents: stats.Documents,
		UpdatedAt: formattedIndexStatsTime(stats.UpdatedAt),
	}
}
