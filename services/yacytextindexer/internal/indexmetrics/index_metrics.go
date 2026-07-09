package indexmetrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const labelReason = "reason"

type IndexMetrics struct {
	registry          *prometheus.Registry
	pagesReceived     prometheus.Counter
	pagesIndexed      prometheus.Counter
	pagesDisposed     *prometheus.CounterVec
	indexFailures     prometheus.Counter
	indexDurationSecs prometheus.Histogram
}

func New() *IndexMetrics {
	registry := prometheus.NewRegistry()
	metrics := &IndexMetrics{
		registry: registry,
		pagesReceived: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "yacytextindexer_pages_received_total",
			Help: "Crawled pages received for indexing.",
		}),
		pagesIndexed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "yacytextindexer_pages_indexed_total",
			Help: "Crawled pages written to the search index.",
		}),
		pagesDisposed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "yacytextindexer_pages_disposed_total",
			Help: "Crawled pages discarded without indexing, by reason.",
		}, []string{labelReason}),
		indexFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "yacytextindexer_index_failures_total",
			Help: "Index writes that failed and returned the page for redelivery.",
		}),
		indexDurationSecs: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "yacytextindexer_index_duration_seconds",
			Help:    "Search-index write duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}),
	}
	registry.MustRegister(
		metrics.pagesReceived,
		metrics.pagesIndexed,
		metrics.pagesDisposed,
		metrics.indexFailures,
		metrics.indexDurationSecs,
	)
	return metrics
}

func (m *IndexMetrics) PageReceived() { m.pagesReceived.Inc() }
func (m *IndexMetrics) PageIndexed()  { m.pagesIndexed.Inc() }

func (m *IndexMetrics) PageDisposed(reason string) {
	m.pagesDisposed.WithLabelValues(reason).Inc()
}

func (m *IndexMetrics) IndexFailed() { m.indexFailures.Inc() }

func (m *IndexMetrics) IndexObserved(elapsed time.Duration) {
	m.indexDurationSecs.Observe(elapsed.Seconds())
}

func (m *IndexMetrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
