package main

import (
	"expvar"
	"net/http"
)

const (
	pathHealth  = "/health"
	pathMetrics = "/metrics"
)

func newOpsMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc(pathHealth, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle(pathMetrics, expvar.Handler())

	return mux
}
