package yagonode

import (
	"net/http"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/bootstrap"
	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/seedimport"
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
	seedStatus, seedRefresh := seedImportSources(assembled, config, recorder)
	blocks := assembledPeerBlocks(assembled)
	options := adminui.Options{
		Overview: newOverviewSource(assembled.report),
		Search:   newSearchSource(assembled.searcher),
		Index:    newIndexSource(assembled.index),
		Network: newNetworkSource(
			assembled.dht.gateStatus,
			assembled.roster,
			config.SeedlistURLs,
			seedStatus,
			blocks,
		),
		Config:          newConfigSource(config),
		Settings:        sources.settings,
		Binding:         sources.binding,
		Logs:            newLogsSource(recorder),
		Security:        sources.security,
		Terms:           newTermSource(assembled.postings, assembled.urlDirectory),
		Schema:          indexSchemaGroups(),
		Performance:     newPerformanceSource(assembled.dht.gateStatus, crawlDepth),
		SeedlistRefresh: seedRefresh,
	}
	applyIndexAdminOptions(&options, assembled)
	if assembled.roster != nil {
		options.PeerDetail = newPeerDetailSource(assembled.roster, blocks)
	}
	if blocks != nil {
		options.PeerBlock = newPeerBlockController(blocks, assembled.identity.Hash)
	}
	if assembled.news != nil {
		options.PeerNews = newPeerNewsSource(assembled.news)
	}
	if dispatcher := crawlDispatcher(assembled.crawl); dispatcher != nil {
		options.Crawl = newCrawlSource(dispatcher)
	}
	if registry := crawlRunRegistry(assembled.crawl); registry != nil {
		options.Monitor = newCrawlMonitorSource(registry, crawlDepth.probe)
		if control := crawlControlRegistry(assembled.crawl); control != nil {
			options.Control = newCrawlControlSource(registry, control)
		}
	}
	opsMux.Handle(adminui.BasePath, adminui.New(options))
	recorder.Record(events.SeverityInfo, events.CategoryConfig, "node.started", "node started")

	return opsMux
}

// seedImportSources opens the durable seed-import status store and, when a roster
// and egress client are available, the operator refresh action over it. A missing
// vault or a store-open failure degrades gracefully to no import history.
func seedImportSources(
	assembled node,
	config nodeConfig,
	recorder *events.Recorder,
) (seedImportStatusReader, adminui.SeedlistRefreshSource) {
	if assembled.vault == nil {
		return nil, nil
	}

	store, err := seedimport.Open(assembled.vault, time.Now)
	if err != nil {
		recorder.Record(events.SeverityWarn, events.CategoryStorage,
			"seedimport.unavailable", "seed import status store unavailable: "+err.Error())

		return nil, nil
	}

	if assembled.roster == nil || assembled.client == nil || len(config.SeedlistURLs) == 0 {
		return store, nil
	}

	refresh := newSeedlistRefreshSource(
		bootstrap.NewSeedlistImporter(assembled.client),
		assembled.roster,
		store,
		recorder,
		config.SeedlistURLs,
	)

	return store, refresh
}

// applyIndexAdminOptions wires the Index console's document browser, delete
// controls, and blacklist manager, each only when its backing store is present.
func applyIndexAdminOptions(options *adminui.Options, assembled node) {
	if assembled.docScan != nil {
		options.Documents = newDocumentBrowseSource(assembled.docScan)
	}
	if assembled.indexAdmin != nil {
		options.IndexAdmin = assembled.indexAdmin
	}
	if assembled.denylist != nil {
		options.Blacklist = newBlacklistController(assembled.denylist)
	}
}

// assembledPeerBlocks returns the peer-block store as an interface, preserving a
// true nil (rather than a typed-nil) when the node has no block store so callers
// can guard on it.
func assembledPeerBlocks(assembled node) peerBlockStore {
	if assembled.peerBlock == nil {
		return nil
	}

	return assembled.peerBlock
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
