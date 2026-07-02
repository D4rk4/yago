package yacysearch

import (
	"encoding/json"
	"net/http"

	"github.com/D4rk4/yago/yacynode/internal/searchcore"
	"github.com/D4rk4/yago/yacyproto"
)

type jsonEndpoint struct {
	search      searchcore.Searcher
	suggestions *recentQueries
}

func MountJSON(mux *http.ServeMux, search searchcore.Searcher) {
	mux.Handle(yacyproto.PathYaCySearchJSON, jsonEndpoint{search: search})
}

func Mount(mux *http.ServeMux, search searchcore.Searcher) {
	suggestions := newRecentQueries()
	mux.Handle(yacyproto.PathYaCySearchJSON, jsonEndpoint{
		search:      search,
		suggestions: suggestions,
	})
	mux.Handle(yacyproto.PathYaCySearchRSS, rssEndpoint{
		search:      search,
		suggestions: suggestions,
	})
	mux.Handle(yacyproto.PathYaCySearchHTML, htmlEndpoint{
		search:      search,
		suggestions: suggestions,
	})
	mux.Handle(yacyproto.PathOpenSearch, openSearchEndpoint{})
	mux.Handle(yacyproto.PathSuggestJSON, suggestEndpoint{suggestions: suggestions})
}

func (e jsonEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	req, err := searchRequestFromValues(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp, err := e.search.Search(r.Context(), req)
	if err != nil {
		http.Error(w, "search failed", http.StatusInternalServerError)
		return
	}
	e.suggestions.Record(req.Query)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(responseJSON(r, resp))
}
