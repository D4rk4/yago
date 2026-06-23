package main

import "net/http"

const (
	pathHealth  = "/health"
	pathMetrics = "/metrics"
)

func newOpsMux(metrics http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc(pathHealth, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle(pathMetrics, metrics)

	return mux
}
