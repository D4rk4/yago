package yagonode

import (
	"net/http"

	"github.com/D4rk4/yago/yacynode/internal/adminui"
	"github.com/D4rk4/yago/yacynode/internal/events"
	"github.com/D4rk4/yago/yacynode/internal/metrics"
)

const (
	pathHealth  = "/health"
	pathMetrics = "/metrics"
)

func buildOpsMux(
	endpoints *metrics.HTTPEndpointMetrics,
	assembled node,
	recorder *events.Recorder,
) *http.ServeMux {
	opsMux := newOpsMux(
		endpoints.Handler(),
		assembled.readiness,
		assembled.dht.gates,
		assembled.indexStats,
		newEventsEndpoint(recorder),
	)
	if assembled.crawl != nil {
		assembled.crawl.mountDispatch(opsMux)
	}
	if assembled.searchExplain != nil {
		opsMux.Handle(pathSearchExplain, assembled.searchExplain)
	}
	opsMux.Handle(adminui.BasePath, adminui.New(adminui.Options{
		Overview: newOverviewSource(assembled.report),
	}))
	recorder.Record(events.SeverityInfo, events.CategoryConfig, "node.started", "node started")

	return opsMux
}

func newOpsMux(
	metrics http.Handler,
	readiness http.Handler,
	dhtGates http.Handler,
	indexStats http.Handler,
	recentEvents http.Handler,
) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc(pathHealth, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	if readiness != nil {
		mux.Handle(pathReady, readiness)
	}
	mux.Handle(pathMetrics, metrics)
	mux.Handle(pathCompatibility, newCompatibilityEndpoint())
	if dhtGates != nil {
		mux.Handle(pathDHTGates, dhtGates)
	}
	if indexStats != nil {
		mux.Handle(pathIndexStats, indexStats)
	}
	if recentEvents != nil {
		mux.Handle(pathEvents, recentEvents)
	}

	return mux
}
