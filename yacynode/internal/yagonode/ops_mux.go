package yagonode

import "net/http"

const (
	pathHealth  = "/health"
	pathMetrics = "/metrics"
)

func newOpsMux(
	metrics http.Handler,
	dhtGates http.Handler,
	indexStats http.Handler,
) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc(pathHealth, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle(pathMetrics, metrics)
	mux.Handle(pathCompatibility, newCompatibilityEndpoint())
	if dhtGates != nil {
		mux.Handle(pathDHTGates, dhtGates)
	}
	if indexStats != nil {
		mux.Handle(pathIndexStats, indexStats)
	}

	return mux
}
