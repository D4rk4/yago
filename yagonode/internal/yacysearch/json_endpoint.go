package yacysearch

import (
	"encoding/json"
	"net/http"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

type jsonEndpoint struct {
	search      searchcore.Searcher
	suggestions *recentQueries
}

func MountJSON(mux *http.ServeMux, search searchcore.Searcher) {
	mux.Handle(yagoproto.PathYaCySearchJSON, jsonEndpoint{search: search})
}

func Mount(mux *http.ServeMux, search searchcore.Searcher, linksNewTab bool) {
	suggestions := newRecentQueries()
	mux.Handle(yagoproto.PathYaCySearchJSON, jsonEndpoint{
		search:      search,
		suggestions: suggestions,
	})
	mux.Handle(yagoproto.PathYaCySearchRSS, rssEndpoint{
		search:      search,
		suggestions: suggestions,
	})
	mux.Handle(yagoproto.PathYaCySearchHTML, htmlEndpoint{
		search:      search,
		suggestions: suggestions,
		newTab:      linksNewTab,
	})
	mux.Handle(yagoproto.PathOpenSearch, openSearchEndpoint{})
	mux.Handle(yagoproto.PathSuggestJSON, suggestEndpoint{suggestions: suggestions})
	mux.Handle(yagoproto.PathSuggestXML, suggestXMLEndpoint{suggestions: suggestions})
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
