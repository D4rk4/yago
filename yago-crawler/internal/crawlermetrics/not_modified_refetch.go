package crawlermetrics

import "github.com/prometheus/client_golang/prometheus"

func newNotModifiedRefetchCounter() prometheus.Counter {
	return prometheus.NewCounter(prometheus.CounterOpts{
		Name: "yacy_crawler_not_modified_refetches_total",
		Help: "Full page fetches started because the node did not record a not-modified observation.",
	})
}

func (m *Metrics) NotModifiedRefetched() { m.notModifiedRefetches.Inc() }
