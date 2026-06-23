package main

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

type endpointMetrics struct {
	registry  *prometheus.Registry
	requests  *prometheus.CounterVec
	durations *prometheus.HistogramVec
}

func newEndpointMetrics() *endpointMetrics {
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

	return &endpointMetrics{registry: registry, requests: requests, durations: durations}
}

func (m *endpointMetrics) observe(endpoint string, status int, elapsed time.Duration) {
	if endpoint == "" {
		endpoint = unmatchedEndpoint
	}
	m.requests.WithLabelValues(endpoint, strconv.Itoa(status)).Inc()
	m.durations.WithLabelValues(endpoint).Observe(elapsed.Seconds())
}

func (m *endpointMetrics) handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
