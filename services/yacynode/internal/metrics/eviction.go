// Package metrics translates domain results into Prometheus series. It is the
// only place that speaks Prometheus; inner packages report plain domain values
// and this edge publishes them.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/eviction"
)

type EvictionMetrics struct {
	urls     prometheus.Counter
	postings prometheus.Counter
	failures prometheus.Counter
}

func NewEvictionMetrics(registry prometheus.Registerer) *EvictionMetrics {
	urls := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "eviction_urls_evicted_total",
		Help: "URLs purged by storage eviction.",
	})
	postings := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "eviction_postings_evicted_total",
		Help: "Postings purged by storage eviction.",
	})
	failures := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "eviction_failures_total",
		Help: "Storage eviction sweeps that ended in error.",
	})
	registry.MustRegister(urls, postings, failures)

	return &EvictionMetrics{urls: urls, postings: postings, failures: failures}
}

func (e *EvictionMetrics) Observe(result eviction.Result) {
	e.urls.Add(float64(result.URLsDeleted))
	e.postings.Add(float64(result.PostingsDeleted))
}

func (e *EvictionMetrics) ObserveFailure() {
	e.failures.Inc()
}
