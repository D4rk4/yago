package metrics

import "github.com/prometheus/client_golang/prometheus"

type SearchMetrics struct {
	requests        prometheus.Counter
	latency         prometheus.Histogram
	results         prometheus.Histogram
	partialFailures prometheus.Counter
}

func NewSearchMetrics(registry prometheus.Registerer) *SearchMetrics {
	requests := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "search_requests_total",
		Help: "Search requests served across every search surface.",
	})
	latency := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "search_latency_seconds",
		Help:    "Search request latency in seconds.",
		Buckets: prometheus.DefBuckets,
	})
	results := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "search_results",
		Help:    "Results returned per search request.",
		Buckets: []float64{0, 1, 5, 10, 20, 50, 100},
	})
	partialFailures := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "search_partial_failures_total",
		Help: "Partial failures reported across search requests.",
	})
	registry.MustRegister(requests, latency, results, partialFailures)

	return &SearchMetrics{
		requests:        requests,
		latency:         latency,
		results:         results,
		partialFailures: partialFailures,
	}
}

func (m *SearchMetrics) Observe(latencySeconds float64, resultCount, failures int) {
	m.requests.Inc()
	m.latency.Observe(latencySeconds)
	m.results.Observe(float64(resultCount))
	m.partialFailures.Add(float64(failures))
}
