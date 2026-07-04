package yagonode

import (
	"net/http"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
)

const (
	pathHealth  = "/health"
	pathMetrics = "/metrics"
)

func buildOpsMux(
	endpoints *metrics.HTTPEndpointMetrics,
	config nodeConfig,
	assembled node,
	recorder *events.Recorder,
	sources consoleAdminSources,
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
	options := adminui.Options{
		Overview: newOverviewSource(assembled.report),
		Search:   newSearchSource(assembled.searcher),
		Index:    newIndexSource(assembled.index),
		Network: newNetworkSource(
			assembled.dht.gateStatus,
			assembled.roster,
			config.SeedlistURLs,
		),
		Config:      newConfigSource(config),
		Settings:    sources.settings,
		Binding:     sources.binding,
		Logs:        newLogsSource(recorder),
		Security:    sources.security,
		Terms:       newTermSource(assembled.postings, assembled.urlDirectory),
		Schema:      indexSchemaGroups(),
		Performance: newPerformanceSource(assembled.dht.gateStatus),
	}
	if dispatcher := crawlDispatcher(assembled.crawl); dispatcher != nil {
		options.Crawl = newCrawlSource(dispatcher)
	}
	opsMux.Handle(adminui.BasePath, adminui.New(options))
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
