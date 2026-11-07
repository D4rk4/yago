package crawlermetrics_test

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlermetrics"
)

func TestMetricsCountNotModifiedRefetchesOnlyWhenReported(t *testing.T) {
	metrics := crawlermetrics.New()
	if body := scrapeMetrics(t, metrics); !strings.Contains(
		body, "yacy_crawler_not_modified_refetches_total 0",
	) {
		t.Fatalf("fresh not-modified refetch series missing or nonzero:\n%s", body)
	}

	metrics.NotModifiedRefetched()

	if body := scrapeMetrics(t, metrics); !strings.Contains(
		body, "yacy_crawler_not_modified_refetches_total 1",
	) {
		t.Fatalf("reported not-modified refetch not counted:\n%s", body)
	}
}
