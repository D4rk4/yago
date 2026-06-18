package infrastructure

import (
	"expvar"
	"net/http"
)

const (
	PathHealth  = "/health"
	PathMetrics = "/metrics"
)

func NewOpsMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc(PathHealth, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle(PathMetrics, expvar.Handler())

	return mux
}
