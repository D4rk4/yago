package yagonode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/D4rk4/yago/yagonode/internal/rankingmodel"
)

const (
	pathSearchRankingModel    = "/api/admin/v1/search/ranking/model"
	pathSearchRankingRollback = "/api/admin/v1/search/ranking/model/rollback"
)

type rankingModelCatalog interface {
	Snapshot() rankingmodel.CatalogSnapshot
	Rollback(context.Context) (bool, error)
}

type rankingModelResponse struct {
	Status         rankingmodel.Status `json:"status"`
	ActiveSnapshot json.RawMessage     `json:"active_snapshot,omitempty"`
}

type searchRankingModelEndpoint struct {
	catalog rankingModelCatalog
}

func newSearchRankingModelEndpoint(catalog rankingModelCatalog) http.Handler {
	return searchRankingModelEndpoint{catalog: catalog}
}

func (endpoint searchRankingModelEndpoint) ServeHTTP(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}
	if endpoint.catalog == nil {
		http.Error(w, "ranking model catalog unavailable", http.StatusServiceUnavailable)

		return
	}
	writeRankingModelResponse(w, endpoint.catalog)
}

type searchRankingRollbackEndpoint struct {
	catalog rankingModelCatalog
}

func newSearchRankingRollbackEndpoint(catalog rankingModelCatalog) http.Handler {
	return searchRankingRollbackEndpoint{catalog: catalog}
}

func (endpoint searchRankingRollbackEndpoint) ServeHTTP(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}
	if endpoint.catalog == nil {
		http.Error(w, "ranking model catalog unavailable", http.StatusServiceUnavailable)

		return
	}
	rolledBack, err := endpoint.catalog.Rollback(r.Context())
	if err != nil {
		http.Error(
			w,
			fmt.Sprintf("rollback ranking model: %v", err),
			http.StatusInternalServerError,
		)

		return
	}
	if !rolledBack {
		http.Error(w, "ranking model rollback history is empty", http.StatusConflict)

		return
	}
	writeRankingModelResponse(w, endpoint.catalog)
}

func writeRankingModelResponse(w http.ResponseWriter, catalog rankingModelCatalog) {
	snapshot := catalog.Snapshot()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rankingModelResponse{
		Status:         snapshot.Status,
		ActiveSnapshot: snapshot.ActiveSnapshot,
	})
}
