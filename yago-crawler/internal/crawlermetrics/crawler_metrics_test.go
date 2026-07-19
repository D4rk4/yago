package crawlermetrics_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlermetrics"
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

func TestMetricsTracksConcurrentActiveFetches(t *testing.T) {
	metrics := crawlermetrics.New()
	const workers = 64
	started := make(chan struct{}, workers)
	release := make(chan struct{})
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			metrics.JobStarted()
			started <- struct{}{}
			<-release
			metrics.JobFinished()
		}()
	}
	for range workers {
		<-started
	}
	if active := metrics.ActiveFetchWorkerJobs(); active != workers {
		t.Fatalf("concurrent active fetches = %d, want %d", active, workers)
	}
	close(release)
	group.Wait()
	if active := metrics.ActiveFetchWorkerJobs(); active != 0 {
		t.Fatalf("finished active fetches = %d, want 0", active)
	}
}

func TestMetricsExposeCrawlerSeries(t *testing.T) {
	metrics := crawlermetrics.New()
	if active := metrics.ActiveFetchWorkerJobs(); active != 0 {
		t.Fatalf("initial active fetches = %d, want 0", active)
	}
	metrics.JobStarted()
	metrics.JobStarted()
	metrics.JobFinished()
	if active := metrics.ActiveFetchWorkerJobs(); active != 1 {
		t.Fatalf("active fetches = %d, want 1", active)
	}
	metrics.FetchAttempted()
	metrics.FetchAttempted()
	metrics.FetchSucceeded(1500)
	metrics.FetchFailed()
	metrics.ParseFailed()
	metrics.RobotsDenied()
	metrics.IngestPublished()
	metrics.ObserveHostBackoff()

	body := scrapeMetrics(t, metrics)
	for _, want := range []string{
		"yacy_crawler_jobs_active 1",
		"yacy_crawler_fetches_total 2",
		"yacy_crawler_bytes_total 1500",
		"yacy_crawler_fetch_failures_total 1",
		"yacy_crawler_parse_failures_total 1",
		"yacy_crawler_robots_denied_total 1",
		"yacy_crawler_ingest_batches_total 1",
		"yacy_crawler_host_backoffs_total 1",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics missing %q in:\n%s", want, body)
		}
	}
}
