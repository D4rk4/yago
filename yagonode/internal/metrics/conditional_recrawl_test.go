package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestCrawlMetricsCountEachConditionalRecrawlOutcomeSeparately(t *testing.T) {
	for _, tc := range []struct {
		name                                  string
		observe                               func(*CrawlMetrics)
		wantIssued, wantRecorded, wantRefused float64
	}{
		{
			name:       "conditional fetch issued",
			observe:    (*CrawlMetrics).ObserveConditionalFetchIssued,
			wantIssued: 1,
		},
		{
			name:         "not-modified observation recorded",
			observe:      (*CrawlMetrics).ObserveNotModifiedRecorded,
			wantRecorded: 1,
		},
		{
			name:        "not-modified observation refused",
			observe:     (*CrawlMetrics).ObserveNotModifiedRefused,
			wantRefused: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			crawl := NewCrawlMetrics(prometheus.NewRegistry())

			tc.observe(crawl)

			outcomes := crawl.conditionalRecrawl
			if issued := testutil.ToFloat64(
				outcomes.conditionalFetchesIssued,
			); issued != tc.wantIssued {
				t.Errorf("conditional fetches issued = %v, want %v", issued, tc.wantIssued)
			}
			if recorded := testutil.ToFloat64(
				outcomes.notModifiedRecorded,
			); recorded != tc.wantRecorded {
				t.Errorf("not-modified recorded = %v, want %v", recorded, tc.wantRecorded)
			}
			if refused := testutil.ToFloat64(
				outcomes.notModifiedRefused,
			); refused != tc.wantRefused {
				t.Errorf("not-modified refused = %v, want %v", refused, tc.wantRefused)
			}
		})
	}
}
