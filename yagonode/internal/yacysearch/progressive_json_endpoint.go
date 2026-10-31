package yacysearch

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type progressiveJSONEndpoint struct {
	search      searchcore.Searcher
	suggestions *recentQueries
}

type progressiveJSONEvent struct {
	Type            string        `json:"type"`
	CompletedShards int           `json:"completed_shards,omitempty"`
	TotalShards     int           `json:"total_shards,omitempty"`
	Complete        bool          `json:"complete"`
	Response        *jsonResponse `json:"response,omitempty"`
	Error           string        `json:"error,omitempty"`
}

func (e progressiveJSONEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unavailable", http.StatusInternalServerError)
		return
	}
	req, err := searchRequestFromValues(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	var writeMu sync.Mutex
	writeEvent := func(event progressiveJSONEvent) bool {
		writeMu.Lock()
		defer writeMu.Unlock()
		if err := json.NewEncoder(w).Encode(event); err != nil {
			return false
		}
		flusher.Flush()

		return true
	}
	if !writeEvent(progressiveJSONEvent{Type: "started"}) {
		cancel()
		return
	}
	ctx = searchcore.WithSearchProgressObserver(ctx, func(progress searchcore.SearchProgress) {
		response := responseJSON(r, progress.Response)
		if !writeEvent(progressiveJSONEvent{
			Type: "progress", CompletedShards: progress.CompletedShards,
			TotalShards: progress.TotalShards, Complete: progress.Complete, Response: &response,
		}) {
			cancel()
		}
	})
	response, err := e.search.Search(ctx, req)
	if err != nil {
		writeEvent(progressiveJSONEvent{Type: "error", Error: "search incomplete"})
		return
	}
	if e.suggestions != nil {
		e.suggestions.Record(req.SubmittedText())
	}
	final := responseJSON(r, response)
	writeEvent(progressiveJSONEvent{
		Type: "final", Complete: final.Complete, Response: &final,
	})
}
