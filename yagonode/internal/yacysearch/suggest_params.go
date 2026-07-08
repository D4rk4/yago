package yacysearch

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

const (
	// suggestMaxCount mirrors upstream YaCy's hard cap on the number of returned
	// suggestions, so a caller cannot ask the index for an unbounded typeahead.
	suggestMaxCount = 30
	// suggestDefaultTimeout mirrors upstream's DidYouMean budget: a typed prefix
	// must not stall the search box, so the index lookup is bounded by this.
	suggestDefaultTimeout = 300 * time.Millisecond
	// suggestMaxTimeout caps a caller-supplied timeout so a hostile request
	// cannot pin the lookup open.
	suggestMaxTimeout = 10 * time.Second
	// suggestCallbackMaxLen bounds a JSONP callback name defensively; real
	// callbacks (jQuery's generated identifiers) are far shorter.
	suggestCallbackMaxLen = 64
)

// suggestParams is the subset of the OpenSearch suggestion request this node
// honours: the query, the requested count (clamped to upstream's 30), the
// lookup timeout (default 300ms), and an optional, validated JSONP callback.
type suggestParams struct {
	query    string
	limit    int
	timeout  time.Duration
	callback string
}

func parseSuggestParams(r *http.Request) suggestParams {
	query := r.URL.Query()

	return suggestParams{
		query:    firstNonEmpty(query.Get("query"), query.Get("q")),
		limit:    suggestCount(query.Get("count")),
		timeout:  suggestTimeout(query.Get("timeout")),
		callback: sanitizeCallback(query.Get("callback")),
	}
}

// suggestCount clamps the requested count to [1, suggestMaxCount]; a missing or
// unparseable value keeps the node's public default.
func suggestCount(raw string) int {
	if raw == "" {
		return publicSuggestionLimit
	}
	count, err := strconv.Atoi(raw)
	if err != nil || count <= 0 {
		return publicSuggestionLimit
	}
	if count > suggestMaxCount {
		return suggestMaxCount
	}

	return count
}

// suggestTimeout reads the millisecond budget, clamped to (0, suggestMaxTimeout];
// a missing or unparseable value keeps upstream's 300ms default.
func suggestTimeout(raw string) time.Duration {
	if raw == "" {
		return suggestDefaultTimeout
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return suggestDefaultTimeout
	}
	budget := time.Duration(ms) * time.Millisecond
	if budget > suggestMaxTimeout {
		return suggestMaxTimeout
	}

	return budget
}

// sanitizeCallback returns the JSONP callback only when it is a bare JavaScript
// identifier, so a reflected callback cannot break out of the wrapping call and
// inject script. Any empty, overlong, or unsafe value disables JSONP wrapping.
func sanitizeCallback(raw string) string {
	if raw == "" || len(raw) > suggestCallbackMaxLen {
		return ""
	}
	for i, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_', r == '$':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return ""
		}
	}

	return raw
}

// mergedSuggestions produces the typeahead list within the request's timeout:
// local-index document titles first, recorded recent queries second. Only the
// index lookup can block, so the timeout bounds it while the in-memory recent
// queries always contribute.
func mergedSuggestions(
	ctx context.Context,
	index indexSuggester,
	recent *recentQueries,
	params suggestParams,
) []string {
	lookupCtx, cancel := context.WithTimeout(ctx, params.timeout)
	defer cancel()

	return mergeSuggestions(
		params.limit,
		index.Suggest(lookupCtx, params.query, params.limit),
		recent.Suggest(params.query, params.limit),
	)
}
