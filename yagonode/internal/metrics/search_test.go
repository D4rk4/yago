package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestSearchMetricsCountsRequestsAndFailures(t *testing.T) {
	search := NewSearchMetrics(prometheus.NewRegistry())

	search.Observe(0.5, 10, 2)
	search.Observe(1.0, 5, 1)

	if got := testutil.ToFloat64(search.requests); got != 2 {
		t.Errorf("requests = %v, want 2", got)
	}
	if got := testutil.ToFloat64(search.partialFailures); got != 3 {
		t.Errorf("partial failures = %v, want 3", got)
	}
	if got := testutil.CollectAndCount(search.latency); got != 1 {
		t.Errorf("latency series = %v, want 1", got)
	}
	if got := testutil.CollectAndCount(search.results); got != 1 {
		t.Errorf("results series = %v, want 1", got)
	}
}
