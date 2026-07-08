package yagonode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/D4rk4/yago/yagonode/internal/judgments"
)

const pathSearchJudgments = "/api/admin/v1/search/judgments"

// judgmentCurator is the CRUD surface the judgments endpoint drives, so an
// operator can build the relevance-judgment set the ranking tuner trains on.
type judgmentCurator interface {
	List(ctx context.Context) ([]judgments.Judgment, error)
	Put(ctx context.Context, judgment judgments.Judgment) error
	Delete(ctx context.Context, query string) (bool, error)
}

type searchJudgmentsEndpoint struct {
	store judgmentCurator
}

func newSearchJudgmentsEndpoint(store judgmentCurator) http.Handler {
	return searchJudgmentsEndpoint{store: store}
}

func (e searchJudgmentsEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if e.store == nil {
		http.Error(w, "judgment store unavailable", http.StatusServiceUnavailable)

		return
	}
	switch r.Method {
	case http.MethodGet:
		e.list(w, r)
	case http.MethodPost:
		e.put(w, r)
	case http.MethodDelete:
		e.remove(w, r)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (e searchJudgmentsEndpoint) list(w http.ResponseWriter, r *http.Request) {
	stored, err := e.store.List(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf("list judgments: %v", err), http.StatusInternalServerError)

		return
	}
	writeJudgmentsJSON(w, stored)
}

func (e searchJudgmentsEndpoint) put(w http.ResponseWriter, r *http.Request) {
	var body judgments.Judgment
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, fmt.Sprintf("decode judgment: %v", err), http.StatusBadRequest)

		return
	}
	if err := e.store.Put(r.Context(), body); err != nil {
		http.Error(w, fmt.Sprintf("store judgment: %v", err), http.StatusBadRequest)

		return
	}
	e.list(w, r)
}

func (e searchJudgmentsEndpoint) remove(w http.ResponseWriter, r *http.Request) {
	removed, err := e.store.Delete(r.Context(), r.URL.Query().Get("query"))
	if err != nil {
		// The %v renders the store error, not the query; this is an HTTP
		// response with no SQL/database involved.
		// nosemgrep
		http.Error(w, fmt.Sprintf("delete judgment: %v", err), http.StatusBadRequest)

		return
	}
	if !removed {
		http.Error(w, "judgment not found", http.StatusNotFound)

		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJudgmentsJSON(w http.ResponseWriter, stored []judgments.Judgment) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Judgments []judgments.Judgment `json:"judgments"`
	}{Judgments: stored})
}
