package yagonode

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

// searchMetricsSearcher meters every search served at the composition chokepoint,
// so the YaCy, Tavily and portal surfaces share one latency, result-count and
// partial-failure view.
type searchMetricsSearcher struct {
	next    searchcore.Searcher
	metrics *metrics.SearchMetrics
	now     func() time.Time
}

func withSearchMetrics(
	next searchcore.Searcher,
	collector *metrics.SearchMetrics,
) searchcore.Searcher {
	if collector == nil {
		return next
	}

	return searchMetricsSearcher{next: next, metrics: collector, now: time.Now}
}

func (s searchMetricsSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	start := s.now()
	response, err := s.next.Search(ctx, req)
	s.metrics.Observe(
		s.now().Sub(start).Seconds(),
		response.TotalResults,
		len(response.PartialFailures),
	)

	return response, err //nolint:wrapcheck // pass the wrapped searcher's error through unchanged.
}
