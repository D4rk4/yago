// Package opsmetrics builds the operator-facing HTTP mux that exposes the
// Prometheus metrics endpoint.
package opsmetrics

import "net/http"

const pathMetrics = "/metrics"

func NewMux(metrics http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle(pathMetrics, metrics)
	return mux
}
