package yagonode

import (
	"context"
	"net/http"

	"github.com/D4rk4/yago/yacynode/internal/crawling"
	"github.com/D4rk4/yago/yacynode/internal/crawlurls"
	"github.com/D4rk4/yago/yacynode/internal/dhtexchange"
	"github.com/D4rk4/yago/yacynode/internal/documentsearch"
	"github.com/D4rk4/yago/yacynode/internal/eviction"
	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacynode/internal/landing"
	"github.com/D4rk4/yago/yacynode/internal/metrics"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacynode/internal/nodestatus"
	"github.com/D4rk4/yago/yacynode/internal/peerannouncement"
	"github.com/D4rk4/yago/yacynode/internal/peerbirth"
	"github.com/D4rk4/yago/yacynode/internal/peernews"
	"github.com/D4rk4/yago/yacynode/internal/peerroster"
	"github.com/D4rk4/yago/yacynode/internal/rwi"
	"github.com/D4rk4/yago/yacynode/internal/tavilyapi"
	"github.com/D4rk4/yago/yacynode/internal/transfertally"
	"github.com/D4rk4/yago/yacynode/internal/urlmeta"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

type node struct {
	peerMux       *http.ServeMux
	readiness     http.Handler
	indexStats    http.Handler
	searchExplain http.Handler
	sweeper       eviction.Sweeper
	announcer     peerannouncement.Announcer
	crawl         crawlProcess
	dht           dhtOutboundProcess
}

type nodeTelemetry struct {
	dhtOutbound      dhtexchange.DistributionObserver
	dhtInbound       *metrics.DHTInboundMetrics
	peer             *metrics.PeerMetrics
	searchAuthorizer tavilyapi.ScopeAuthorizer
}

var (
	openRuntimeNodeStorage      = openNodeStorage
	openRuntimePeerBirthDate    = peerbirth.Open
	openRuntimePeerNews         = peernews.Open
	openRuntimeTransferTally    = transfertally.Open
	assembleRuntimePeerExchange = func(exchange peerExchange) (peerExchangeRuntime, error) {
		return exchange.assemble()
	}
	buildRuntimeDHTOutbound = buildDHTOutboundRuntime
	buildRuntimeCrawl       = func(
		config crawlConfig,
		identity nodeidentity.Identity,
		storage nodeStorage,
		storageVault *vault.Vault,
	) (crawlProcess, error) {
		runtime, err := buildCrawlRuntime(config, identity, storage, storageVault)
		if runtime == nil || err != nil {
			return nil, err
		}

		return runtime, nil
	}
)

func assembleNode(
	ctx context.Context,
	config nodeConfig,
	vault *vault.Vault,
	client *http.Client,
	telemetry nodeTelemetry,
) (node, error) {
	identity, err := nodeIdentityWithBirthDate(ctx, config, vault)
	if err != nil {
		return node{}, err
	}

	storage, err := openRuntimeNodeStorage(vault, config.SearchIndexPath)
	if err != nil {
		return node{}, err
	}

	roster, news, tally, err := openPeerStores(vault, telemetry.peer)
	if err != nil {
		return node{}, err
	}

	report := newNodeStatusReport(identity, storage, roster, news, tally)
	storage = observeDHTInboundStorage(storage, telemetry.dhtInbound, tally)

	mux := http.NewServeMux()
	mux.Handle("/{$}", landing.NewEndpoint())
	router := httpguard.NewWireRouter(mux, newRuntimeWireGate(config, report))

	mountNodeProtocol(router, identity, storage)
	mountNodeCrawlCompatibility(router, identity, storage)

	exchange, err := assembleRuntimePeerExchange(peerExchange{
		router:   router,
		identity: identity,
		report:   report,
		config:   config,
		vault:    vault,
		client:   client,
		peer:     telemetry.peer,
		host:     storedDocumentHostLinks{documents: storage.storedDocuments()},
		roster:   roster,
		news:     news,
	})
	if err != nil {
		return node{}, err
	}
	surfaces, err := assembleNodeSurfaces(assembleSurfacesInput{
		ctx:       ctx,
		config:    config,
		vault:     vault,
		client:    client,
		mux:       mux,
		storage:   storage,
		roster:    roster,
		identity:  identity,
		report:    report,
		tally:     tally,
		telemetry: telemetry,
	})
	if err != nil {
		return node{}, err
	}

	return newAssembledNode(nodeParts{
		mux:       mux,
		storage:   storage,
		announcer: exchange.announcer,
		crawl:     surfaces.crawl,
		dht:       surfaces.dht,
		vault:     vault,
	}), nil
}

type assembleSurfacesInput struct {
	ctx       context.Context
	config    nodeConfig
	vault     *vault.Vault
	client    *http.Client
	mux       *http.ServeMux
	storage   nodeStorage
	roster    peerroster.Roster
	identity  nodeidentity.Identity
	report    nodestatus.Report
	tally     *transfertally.Tally
	telemetry nodeTelemetry
}

type nodeSurfaces struct {
	crawl crawlProcess
	dht   dhtOutboundProcess
}

func assembleNodeSurfaces(in assembleSurfacesInput) (nodeSurfaces, error) {
	runtime, err := buildRuntimeCrawl(in.config.Crawl, in.identity, in.storage, in.vault)
	if err != nil {
		return nodeSurfaces{}, err
	}
	mountNodePublicSearch(in.mux, publicSearchAssembly{
		storage:          in.storage,
		roster:           in.roster,
		identity:         in.identity,
		dht:              in.config.DHT,
		client:           in.client,
		searchAPIKey:     in.config.SearchAPIKey,
		searchAuthorizer: in.telemetry.searchAuthorizer,
		extractFetcher:   buildExtractFetcher(in.config, in.client),
		webFallback:      in.config.WebFallback,
		seedQueue:        crawlOrderQueue(runtime),
	})
	dht := buildRuntimeDHTOutbound(dhtOutboundRuntimeAssembly{
		ctx:         in.ctx,
		config:      in.config,
		storage:     in.vault,
		nodeStorage: in.storage,
		report:      in.report,
		roster:      in.roster,
		client:      in.client,
		observer:    tallyOutboundObserver{next: in.telemetry.dhtOutbound, tally: in.tally},
	})

	return nodeSurfaces{crawl: runtime, dht: dht}, nil
}

type nodeParts struct {
	mux       *http.ServeMux
	storage   nodeStorage
	announcer peerannouncement.Announcer
	crawl     crawlProcess
	dht       dhtOutboundProcess
	vault     *vault.Vault
}

func newAssembledNode(parts nodeParts) node {
	return node{
		peerMux:       parts.mux,
		readiness:     newReadinessEndpoint(parts.storage.searchIndex),
		indexStats:    newIndexStatsEndpoint(parts.storage.searchIndex),
		searchExplain: newSearchExplainEndpoint(parts.storage.searchIndex),
		sweeper:       newStorageSweeper(parts.vault, parts.storage),
		announcer:     parts.announcer,
		crawl:         parts.crawl,
		dht:           parts.dht,
	}
}

func newRuntimeWireGate(
	config nodeConfig,
	report nodestatus.Report,
) httpguard.WireGate {
	return httpguard.WireGate{
		Guard: httpguard.NewRequestGuard(
			httpguard.DefaultMaxBodyBytes,
			httpguard.DefaultRequestTimeout,
		),
		Respond: httpguard.NewWireResponder(report),
		Address: httpguard.NewClientAddressResolver(config.TrustedProxies),
	}
}

func mountNodeCrawlCompatibility(
	router httpguard.WireRouter,
	identity nodeidentity.Identity,
	storage nodeStorage,
) {
	crawling.MountCrawlReceipt(router, identity)
	crawlurls.Mount(router, identity, storage.urlDirectory, crawlurls.DisabledRemoteCrawlURLs{})
}

func mountNodeProtocol(
	router httpguard.WireRouter,
	identity nodeidentity.Identity,
	storage nodeStorage,
) {
	urlmeta.MountTransferURL(router, identity, storage.urlReceiver)
	rwi.MountTransferRWI(router, identity, storage.postingReceiver)
	nodestatus.MountQuery(
		router,
		identity,
		storage.postings,
		storage.urlDirectory,
	)
	documentsearch.MountSearch(
		router,
		identity,
		storage.postings,
		storage.urlDirectory,
		searchPostingsPerWord,
	)
}

func newStorageSweeper(vault *vault.Vault, storage nodeStorage) eviction.Sweeper {
	return eviction.NewSweeper(
		vault,
		storage.postingPurger,
		storage.references,
		storage.urlEvictor,
		storage.staleness,
		eviction.Config{TargetFraction: evictionTargetFraction, BatchSize: evictionBatch},
	)
}
