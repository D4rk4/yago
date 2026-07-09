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

// ObserveExemplar records a completed request like Observe — counting it by
// endpoint and status — but records the single latency observation with the
// sampled trace ID pinned on as an exemplar, so a slow bucket links straight to
// a live trace (OPS-10). A sampled request records exactly one histogram
// observation carrying the exemplar: callers use this instead of Observe for a
// sampled request, never in addition, so the latency histogram is not
// double-counted. It falls back to a plain observation when no trace ID is
// given or the histogram cannot carry exemplars.
func (e *HTTPEndpointMetrics) ObserveExemplar(
	endpoint string,
	status int,
	elapsed time.Duration,
	traceID string,
) {
	if endpoint == "" {
		endpoint = unmatchedEndpoint
	}
	e.requests.WithLabelValues(endpoint, strconv.Itoa(status)).Inc()
	observer := e.durations.WithLabelValues(endpoint)
	exemplar, ok := observer.(prometheus.ExemplarObserver)
	if !ok || traceID == "" {
		observer.Observe(elapsed.Seconds())

		return
	}
	exemplar.ObserveWithExemplar(elapsed.Seconds(), prometheus.Labels{"trace_id": traceID})
}

func (e *HTTPEndpointMetrics) Handler() http.Handler {
	return promhttp.HandlerFor(e.registry, promhttp.HandlerOpts{})
}
