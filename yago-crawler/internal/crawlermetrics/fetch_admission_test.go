package crawlermetrics_test

import (
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlermetrics"
)

// The fleet fetch-start permit and the process page-rate budget bound crawl
// throughput, and neither was measured: a worker parked on a governor is still
// an active job, so "busy" and "rate-limited" looked identical on
// yacy_crawler_jobs_active. These series separate them.
func TestMetricsExposeFetchAdmissionWaits(t *testing.T) {
	metrics := crawlermetrics.New()
	metrics.FetchAdmissionWaitStarted()
	metrics.FetchAdmissionWaitStarted()
	metrics.FetchAdmissionWaitFinished()
	metrics.ObserveFetchAdmissionWait(250 * time.Millisecond)

	body := scrapeMetrics(t, metrics)

	if !strings.Contains(body, "\nyacy_crawler_fetch_admission_waiting 1\n") {
		t.Fatalf("metrics missing the waiting gauge:\n%s", body)
	}
	if !strings.Contains(body, "yacy_crawler_fetch_admission_seconds_count 1") {
		t.Fatalf("metrics missing the admission wait histogram:\n%s", body)
	}
	if !strings.Contains(body, "yacy_crawler_fetch_admission_seconds_sum 0.25") {
		t.Fatalf("admission wait histogram did not record the elapsed time:\n%s", body)
	}
}
