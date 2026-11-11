package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestCrawlRunMetricsSetActive(t *testing.T) {
	m := NewCrawlRunMetrics(prometheus.NewRegistry())

	m.SetActive(4)
	if got := testutil.ToFloat64(m.active); got != 4 {
		t.Fatalf("active = %v, want 4", got)
	}

	m.SetActive(1)
	if got := testutil.ToFloat64(m.active); got != 1 {
		t.Fatalf("active after update = %v, want 1", got)
	}
}

func TestCrawlRunMetricsObserveTerminal(t *testing.T) {
	m := NewCrawlRunMetrics(prometheus.NewRegistry())

	m.ObserveTerminal(yagocrawlcontract.CrawlRunFinished, yagocrawlcontract.CrawlRunTally{
		Fetched:            10,
		Indexed:            7,
		Failed:             2,
		RobotsDenied:       1,
		Duplicates:         3,
		DocumentCandidates: 8,
		RemovalMutations:   1,
		NotIndexable:       1,
		NodeRejected:       1,
		PendingIngest:      1,
		OutcomeCounts: []yagocrawlcontract.CrawlOutcomeCount{
			{Class: "not_indexable", Reason: "sitemap", Pages: 1},
			{Class: "node_rejected", Reason: "too-few-words", Pages: 1},
		},
	})
	m.ObserveTerminal(yagocrawlcontract.CrawlRunCancelled, yagocrawlcontract.CrawlRunTally{
		Fetched: 5,
	})

	cases := map[string]float64{
		"finished":  1,
		"cancelled": 1,
	}
	for state, want := range cases {
		if got := testutil.ToFloat64(m.terminal.WithLabelValues(state)); got != want {
			t.Fatalf("terminal[%s] = %v, want %v", state, got, want)
		}
	}

	outcomes := map[string]float64{
		"fetched":             15,
		"indexed":             7,
		"failed":              2,
		"robots_denied":       1,
		"duplicates":          3,
		"document_candidates": 8,
		"removal_mutations":   1,
		"not_indexable":       1,
		"node_rejected":       1,
		"unknown":             5,
	}
	for outcome, want := range outcomes {
		if got := testutil.ToFloat64(m.outcomes.WithLabelValues(outcome)); got != want {
			t.Fatalf("outcomes[%s] = %v, want %v", outcome, got, want)
		}
	}
	if got := testutil.ToFloat64(m.pageOutcomeReasons.WithLabelValues("not_indexable", "sitemap")); got != 1 {
		t.Fatalf("sitemap no-mutation outcomes = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.pageOutcomeReasons.WithLabelValues("node_rejected", "too-few-words")); got != 1 {
		t.Fatalf("node rejection outcomes = %v, want 1", got)
	}
}
