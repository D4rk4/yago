package searchsession

import (
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type sessionWindow struct {
	results    []searchcore.Result
	failures   []searchcore.PartialFailure
	total      int
	recovered  string
	didYouMean string
	facets     []searchcore.FacetGroup
}

func (e *session) replaceVisibleWindowLocked() {
	e.visible.Store(&sessionWindow{
		results:    e.results,
		failures:   e.failures,
		total:      e.total,
		recovered:  e.recovered,
		didYouMean: e.didYouMean,
		facets:     e.facets,
	})
}

func (w *sessionWindow) respond(req searchcore.Request) searchcore.Response {
	limit := req.Limit
	if limit <= 0 {
		limit = searchcore.DefaultPublicLimit
	}
	start := req.Offset
	if start > len(w.results) {
		start = len(w.results)
	}
	end := start + limit
	if end > len(w.results) {
		end = len(w.results)
	}

	return searchcore.Response{
		Request:         req,
		TotalResults:    w.total,
		Results:         cloneSessionResults(w.results[start:end]),
		PartialFailures: cloneSessionFailures(w.failures),
		Recovered:       strings.Clone(w.recovered),
		DidYouMean:      strings.Clone(w.didYouMean),
		Facets:          cloneSessionFacets(w.facets),
	}
}
