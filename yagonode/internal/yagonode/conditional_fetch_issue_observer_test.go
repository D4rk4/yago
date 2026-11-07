package yagonode

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
)

type countingConditionalFetchIssues struct {
	issued int
}

func (c *countingConditionalFetchIssues) ObserveConditionalFetchIssued() { c.issued++ }

func TestRecrawlSweepObservesOnlyPublishedConditionalFetches(t *testing.T) {
	for _, tc := range []struct {
		name       string
		validators *yagocrawlcontract.HTTPValidators
		publisher  recrawlPublisher
		wantIssued int
	}{
		{
			name:       "conditional fetch published",
			validators: firstValidators(),
			publisher:  &capturingPublisher{},
			wantIssued: 1,
		},
		{
			name:      "unconditional fetch published",
			publisher: &capturingPublisher{},
		},
		{
			name:       "conditional fetch not published",
			validators: firstValidators(),
			publisher:  failingPublisher{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := openConditionalRecrawlNode(t, filepath.Join(t.TempDir(), "node.db"), time.Hour)
			if outcome := node.submit(
				t, fullConditionalPage(t, conditionalBase, "first text", tc.validators),
			); outcome != "ack" {
				t.Fatalf("full page settlement = %q, want ack", outcome)
			}
			issues := &countingConditionalFetchIssues{}

			recrawlSweeper{
				frontier:               node.ingest.frontier,
				publisher:              tc.publisher,
				mint:                   func() []byte { return []byte("recrawl") },
				now:                    func() time.Time { return conditionalBase.Add(time.Hour) },
				batch:                  8,
				conditional:            node.ingest.conditional,
				conditionalFetchIssues: issues,
			}.sweepOnce(context.Background())

			if issues.issued != tc.wantIssued {
				t.Fatalf("conditional fetches observed = %d, want %d", issues.issued, tc.wantIssued)
			}
		})
	}
}

func TestCrawlMetricsReachTheRecrawlSweeper(t *testing.T) {
	runtime := liveCrawlRuntime(t)
	if issues := runtime.recrawlSweeper().conditionalFetchIssues; issues != nil {
		t.Fatalf("unobserved sweeper reports conditional fetches to %T", issues)
	}
	collector := metrics.NewCrawlMetrics(prometheus.NewRegistry())

	attachCrawlMetrics(runtime, collector)

	if issues := runtime.recrawlSweeper().conditionalFetchIssues; issues != collector {
		t.Fatalf(
			"observed sweeper reports conditional fetches to %v, want the crawl metrics",
			issues,
		)
	}
}
