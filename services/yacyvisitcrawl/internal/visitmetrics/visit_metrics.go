// Package visitmetrics exposes the visit intake service's operational metrics
// through a Prometheus registry. New builds the registry; Handler serves it.
package visitmetrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type VisitMetrics struct {
	registry       *prometheus.Registry
	visitsReceived prometheus.Counter
	visitsRejected prometheus.Counter
	ordersPlaced   prometheus.Counter
	ordersUnplaced prometheus.Counter
}

func New() *VisitMetrics {
	registry := prometheus.NewRegistry()
	metrics := &VisitMetrics{
		registry: registry,
		visitsReceived: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "yacyvisitcrawl_visits_received_total",
			Help: "Visited-page requests received.",
		}),
		visitsRejected: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "yacyvisitcrawl_visits_rejected_total",
			Help: "Visited-page requests rejected for an invalid url.",
		}),
		ordersPlaced: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "yacyvisitcrawl_orders_placed_total",
			Help: "Crawl orders placed on the broker.",
		}),
		ordersUnplaced: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "yacyvisitcrawl_orders_unplaced_total",
			Help: "Crawl orders that could not be placed on the broker.",
		}),
	}
	registry.MustRegister(
		metrics.visitsReceived,
		metrics.visitsRejected,
		metrics.ordersPlaced,
		metrics.ordersUnplaced,
	)
	return metrics
}

func (m *VisitMetrics) VisitReceived() { m.visitsReceived.Inc() }
func (m *VisitMetrics) VisitRejected() { m.visitsRejected.Inc() }
func (m *VisitMetrics) OrderPlaced()   { m.ordersPlaced.Inc() }
func (m *VisitMetrics) OrderUnplaced() { m.ordersUnplaced.Inc() }

func (m *VisitMetrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
