package yacysearch

import (
	"net/http"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

// NewSuggestHandler serves OpenSearch suggestion JSON from the given searcher —
// the same index-backed typeahead the public surfaces use — so other listeners
// (the admin console) can mount autocomplete without exposing the public
// endpoints. The searcher should be local-only; suggestions never fan out.
func NewSuggestHandler(search searchcore.Searcher) http.Handler {
	return suggestEndpoint{
		index:       indexSuggester{search: search},
		suggestions: newRecentQueries(),
	}
}
