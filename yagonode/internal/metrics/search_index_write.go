package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type SearchIndexWriteMetrics struct {
	duration  prometheus.Histogram
	documents prometheus.Counter
	failures  prometheus.Counter
}

func NewSearchIndexWriteMetrics(registry prometheus.Registerer) *SearchIndexWriteMetrics {
	duration := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "crawl_search_index_write_duration_seconds",
		Help:    "Duration of crawl search-index write attempts in seconds.",
		Buckets: prometheus.DefBuckets,
	})
	documents := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "crawl_search_index_documents_total",
		Help: "Documents successfully written to the local search index by crawl ingest.",
	})
	failures := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "crawl_search_index_write_failures_total",
		Help: "Crawl search-index write attempts that failed.",
	})
	registry.MustRegister(duration, documents, failures)

	return &SearchIndexWriteMetrics{
		duration:  duration,
		documents: documents,
		failures:  failures,
	}
}

func (m *SearchIndexWriteMetrics) ObserveSearchIndexWrite(
	duration time.Duration,
	documents int,
	failed bool,
) {
	m.duration.Observe(duration.Seconds())
	if failed {
		m.failures.Inc()

		return
	}
	m.documents.Add(float64(documents))
}
