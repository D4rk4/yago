package publicratelimit

import (
	"net/http"
	"strings"
)

const maximumConcurrentPublicSearches = 16

type publicSearchRequestKind uint8

const (
	untrackedPublicRequest publicSearchRequestKind = iota
	trackedPublicInteraction
	expensivePublicSearchRequest
)

var publicSearchAdmission = make(chan struct{}, maximumConcurrentPublicSearches)

func classifyPublicSearchRequest(r *http.Request) publicSearchRequestKind {
	path := r.URL.Path
	switch {
	case strings.HasPrefix(path, "/yacysearch."):
		return expensivePublicSearchRequest
	case path == "/suggest.json" || path == "/opensearch/suggest":
		return expensivePublicSearchRequest
	case path == "/" && r.URL.Query().Get("q") != "":
		return expensivePublicSearchRequest
	case path == "/searchclick":
		return trackedPublicInteraction
	default:
		return untrackedPublicRequest
	}
}

func AdmitSearch() (func(), bool) {
	select {
	case publicSearchAdmission <- struct{}{}:
		return func() { <-publicSearchAdmission }, true
	default:
		return nil, false
	}
}
