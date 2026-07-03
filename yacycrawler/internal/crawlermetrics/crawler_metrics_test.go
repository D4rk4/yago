package crawlermetrics_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yacycrawler/internal/crawlermetrics"
)

func scrapeMetrics(t *testing.T, metrics *crawlermetrics.Metrics) string {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(rec, req)
	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatalf("read metrics: %v", err)
	}

	return string(body)
}

func TestMetricsExposeCrawlerSeries(t *testing.T) {
	metrics := crawlermetrics.New()
	metrics.JobStarted()
	metrics.JobStarted()
	metrics.JobFinished()
	metrics.FetchAttempted()
	metrics.FetchAttempted()
	metrics.FetchSucceeded(1500)
	metrics.FetchFailed()
	metrics.RobotsDenied()
	metrics.IngestPublished()

	body := scrapeMetrics(t, metrics)
	for _, want := range []string{
		"yacy_crawler_jobs_active 1",
		"yacy_crawler_fetches_total 2",
		"yacy_crawler_bytes_total 1500",
		"yacy_crawler_fetch_failures_total 1",
		"yacy_crawler_robots_denied_total 1",
		"yacy_crawler_ingest_batches_total 1",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics missing %q in:\n%s", want, body)
		}
	}
}
