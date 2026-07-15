package yagonode

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/D4rk4/yago/yagonode/internal/rankingprofile"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

const pathSearchRanking = "/api/admin/v1/search/ranking"

type searchRankingEndpoint struct {
	holder *rankingprofile.Holder
}

type searchRankingRequest struct {
	Weights searchindex.RankingWeights `json:"weights"`
}

type searchRankingResponse struct {
	Weights searchindex.RankingWeights `json:"weights"`
}

func newSearchRankingEndpoint(holder *rankingprofile.Holder) http.Handler {
	return searchRankingEndpoint{holder: holder}
}

func (e searchRankingEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if e.holder == nil {
		http.Error(w, "ranking profile unavailable", http.StatusServiceUnavailable)

		return
	}
	switch r.Method {
	case http.MethodGet:
		writeRankingJSON(w, e.holder.Current())
	case http.MethodPost:
		e.update(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (e searchRankingEndpoint) update(w http.ResponseWriter, r *http.Request) {
	body := searchRankingRequest{Weights: e.holder.Current()}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, fmt.Sprintf("decode request: %v", err), http.StatusBadRequest)

		return
	}
	if err := e.holder.Set(r.Context(), body.Weights); err != nil {
		http.Error(w, fmt.Sprintf("apply ranking profile: %v", err), http.StatusBadRequest)

		return
	}
	writeRankingJSON(w, e.holder.Current())
}

func writeRankingJSON(w http.ResponseWriter, weights searchindex.RankingWeights) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(searchRankingResponse{Weights: weights})
}
