package yacysearch

import (
	"encoding/json"
	"net/http"
)

type suggestEndpoint struct {
	index       indexSuggester
	suggestions *recentQueries
}

func (e suggestEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := firstNonEmpty(r.URL.Query().Get("query"), r.URL.Query().Get("q"))
	w.Header().Set("Content-Type", "application/x-suggestions+json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode([]any{
		query,
		mergeSuggestions(
			publicSuggestionLimit,
			e.index.Suggest(r.Context(), query, publicSuggestionLimit),
			e.suggestions.Suggest(query, publicSuggestionLimit),
		),
	})
}
