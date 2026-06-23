// Package metrics translates domain results into Prometheus series. It is the
// only place that speaks Prometheus; inner packages report plain domain values
// and this edge publishes them.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/eviction"
)

type Eviction struct {
	urls     prometheus.Counter
	postings prometheus.Counter
	failures prometheus.Counter
}

func NewEviction(registry prometheus.Registerer) *Eviction {
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

	return &Eviction{urls: urls, postings: postings, failures: failures}
}

func (e *Eviction) Observe(result eviction.Result) {
	e.urls.Add(float64(result.URLsDeleted))
	e.postings.Add(float64(result.PostingsDeleted))
}

func (e *Eviction) ObserveFailure() {
	e.failures.Inc()
}
