package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	labelEndpoint     = "endpoint"
	labelStatusCode   = "code"
	unmatchedEndpoint = "unmatched"
)

type HTTPEndpointMetrics struct {
	registry  *prometheus.Registry
	requests  *prometheus.CounterVec
	durations *prometheus.HistogramVec
}

func NewHTTPEndpointMetrics() *HTTPEndpointMetrics {
	registry := prometheus.NewRegistry()
	requests := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "HTTP requests served, by endpoint and response status code.",
		},
		[]string{labelEndpoint, labelStatusCode},
	)
	durations := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds, by endpoint.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{labelEndpoint},
	)
	registry.MustRegister(requests, durations)

	return &HTTPEndpointMetrics{registry: registry, requests: requests, durations: durations}
}

func (e *HTTPEndpointMetrics) Registry() *prometheus.Registry {
	return e.registry
}

func (e *HTTPEndpointMetrics) Observe(endpoint string, status int, elapsed time.Duration) {
	if endpoint == "" {
		endpoint = unmatchedEndpoint
	}
	e.requests.WithLabelValues(endpoint, strconv.Itoa(status)).Inc()
	e.durations.WithLabelValues(endpoint).Observe(elapsed.Seconds())
}

// ObserveExemplar pins a sampled trace ID onto the latency histogram bucket
// this request fell into, so a slow bucket links straight to a live trace
// (OPS-10). Non-exemplar observers ignore the call.
func (e *HTTPEndpointMetrics) ObserveExemplar(
	endpoint string,
	elapsed time.Duration,
	traceID string,
) {
	if endpoint == "" {
		endpoint = unmatchedEndpoint
	}
	observer, ok := e.durations.WithLabelValues(endpoint).(prometheus.ExemplarObserver)
	if !ok {
		return
	}
	observer.ObserveWithExemplar(elapsed.Seconds(), prometheus.Labels{"trace_id": traceID})
}

func (e *HTTPEndpointMetrics) Handler() http.Handler {
	return promhttp.HandlerFor(e.registry, promhttp.HandlerOpts{})
}
