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
	crawlDepth := crawlQueueDepthSource{probe: crawlQueueProbe(assembled.crawl)}
	metrics.NewQueueDepthMetrics(
		endpoints.Registry(),
		newQueueDepthSource(assembled.dht.gateStatus, crawlDepth),
	)
	opsMux := newOpsMux(
		metricsHandler(endpoints, config.MetricsEnabled),
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
	if assembled.searchRanking != nil {
		opsMux.Handle(pathSearchRanking, assembled.searchRanking)
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
		Performance: newPerformanceSource(assembled.dht.gateStatus, crawlDepth),
	}
	if dispatcher := crawlDispatcher(assembled.crawl); dispatcher != nil {
		options.Crawl = newCrawlSource(dispatcher)
	}
	opsMux.Handle(adminui.BasePath, adminui.New(options))
	recorder.Record(events.SeverityInfo, events.CategoryConfig, "node.started", "node started")

	return opsMux
}

func metricsHandler(endpoints *metrics.HTTPEndpointMetrics, enabled bool) http.Handler {
	if !enabled {
		return nil
	}

	return endpoints.Handler()
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
	if metrics != nil {
		mux.Handle(pathMetrics, metrics)
	}
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
