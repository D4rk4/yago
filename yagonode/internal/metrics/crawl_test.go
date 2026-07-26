package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestCrawlMetricsCountsIngest(t *testing.T) {
	crawl := NewCrawlMetrics(prometheus.NewRegistry())

	crawl.ObserveAbsorbed(120, 3, 40)
	crawl.ObserveAbsorbed(80, 1, 10)
	crawl.ObserveDeferred()
	crawl.ObserveRejected()
	crawl.ObserveRejected()
	crawl.ObserveLowQuality()

	for _, tc := range []struct {
		name    string
		counter prometheus.Counter
		want    float64
	}{
		{"absorbed", crawl.absorbed, 2},
		{"deferred", crawl.deferred, 1},
		{"rejected", crawl.rejected, 2},
		{"lowQuality", crawl.lowQuality, 1},
		{"bytes", crawl.bytes, 200},
		{"urls", crawl.urls, 4},
		{"postings", crawl.postings, 50},
	} {
		if got := testutil.ToFloat64(tc.counter); got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A page can be stored and searchable while its durable crawl profile or
// recrawl entry fails to write. The write is best-effort so it cannot roll back
// an indexed document, which is exactly why the gap needs a counter to alert on
// rather than only a log line.
func TestCrawlMetricsCountScheduleFailures(t *testing.T) {
	crawl := NewCrawlMetrics(prometheus.NewRegistry())

	crawl.ObserveScheduleFailure()
	crawl.ObserveScheduleFailure()

	if got := testutil.ToFloat64(crawl.scheduleFailures); got != 2 {
		t.Fatalf("schedule failures = %v, want 2", got)
	}
}
