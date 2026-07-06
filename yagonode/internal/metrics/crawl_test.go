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
	crawl.ObserveDuplicate()
	crawl.ObserveLowQuality()

	for _, tc := range []struct {
		name    string
		counter prometheus.Counter
		want    float64
	}{
		{"absorbed", crawl.absorbed, 2},
		{"deferred", crawl.deferred, 1},
		{"rejected", crawl.rejected, 2},
		{"duplicates", crawl.duplicates, 1},
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
