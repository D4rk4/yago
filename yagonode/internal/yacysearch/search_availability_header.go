package yacysearch

import (
	"net/http"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const (
	searchAvailabilityHeader     = "X-Yago-Search-Availability"
	searchAvailabilityIncomplete = "incomplete"
	searchAvailabilityRetryAfter = "1"
)

// markUnprovenZero labels an empty answer the node cannot vouch for. The status
// stays 200 and the body is untouched: /yacysearch.* is a YaCy-compatible
// surface, and third-party OpenSearch clients -- including this project's own
// end-to-end poller -- read any other status as "keep polling". A caller that
// wants to distinguish "nothing matched" from "nothing answered" reads the
// header; every existing caller sees exactly what it saw before.
func markUnprovenZero(w http.ResponseWriter, resp searchcore.Response) {
	if !resp.UnprovenZero() {
		return
	}
	w.Header().Set(searchAvailabilityHeader, searchAvailabilityIncomplete)
	w.Header().Set("Retry-After", searchAvailabilityRetryAfter)
}
