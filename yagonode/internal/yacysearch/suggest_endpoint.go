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

	params := parseSuggestParams(r)
	// A [query, []suggestions] array of strings always marshals, so the error is
	// ignored the way the endpoint already ignores its encode result.
	body, _ := json.Marshal([]any{
		params.query,
		mergedSuggestions(r.Context(), e.index, e.suggestions, params),
	})

	// Suggestions are public data meant to be consumed by cross-origin search
	// boxes, so advertise open CORS the way upstream does for the XML form.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	contentType := "application/x-suggestions+json"
	if params.callback != "" {
		contentType = "application/javascript; charset=utf-8"
		body = jsonpWrap(params.callback, body)
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	// body is json.Marshal output served as JSON or JS (never text/html), and a
	// JSONP callback is a validated bare identifier, so there is nothing to
	// HTML-escape here.
	// nosemgrep
	_, _ = w.Write(body)
}

// jsonpWrap wraps a marshalled suggestion array in the caller's callback. The
// callback has already been validated to a bare JavaScript identifier by
// sanitizeCallback, so it cannot escape the wrapping call.
func jsonpWrap(callback string, body []byte) []byte {
	wrapped := make([]byte, 0, len(callback)+len(body)+3)
	wrapped = append(wrapped, callback...)
	wrapped = append(wrapped, '(')
	wrapped = append(wrapped, body...)
	wrapped = append(wrapped, ')', ';')

	return wrapped
}
