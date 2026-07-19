package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/D4rk4/yago/yagonode/internal/remotecrawl"
)

func TestRemoteCrawlMetricsCountDecisionsAndItems(t *testing.T) {
	collector := NewRemoteCrawlMetrics(prometheus.NewRegistry())
	collector.ObserveRemoteCrawl(remotecrawl.Observation{
		Action: "lease", Outcome: "accepted", Count: 3,
	})
	collector.ObserveRemoteCrawl(remotecrawl.Observation{
		Action: "lease", Outcome: "rate_limited",
	})
	if got := testutil.ToFloat64(
		collector.decisions.WithLabelValues("lease", "accepted"),
	); got != 3 {
		t.Fatalf("accepted decisions = %v, want 3", got)
	}
	if got := testutil.ToFloat64(
		collector.decisions.WithLabelValues("lease", "rate_limited"),
	); got != 1 {
		t.Fatalf("rejected decisions = %v, want 1", got)
	}
}
