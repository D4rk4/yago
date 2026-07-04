package yagonode

import (
	"context"
	"fmt"
	"net/http"

	"github.com/D4rk4/yago/yagonode/internal/crawling"
	"github.com/D4rk4/yago/yagonode/internal/crawlurls"
	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
	"github.com/D4rk4/yago/yagonode/internal/documentsearch"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/eviction"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/nodestatus"
	"github.com/D4rk4/yago/yagonode/internal/peerannouncement"
	"github.com/D4rk4/yago/yagonode/internal/peerbirth"
	"github.com/D4rk4/yago/yagonode/internal/peerblock"
	"github.com/D4rk4/yago/yagonode/internal/peernews"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
	"github.com/D4rk4/yago/yagonode/internal/rankingprofile"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/tavilyapi"
	"github.com/D4rk4/yago/yagonode/internal/transfertally"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type node struct {
	peerMux       *http.ServeMux
	readiness     http.Handler
	indexStats    http.Handler
	searchExplain http.Handler
	searchRanking http.Handler
	report        nodestatus.Report
	searcher      searchcore.Searcher
	index         searchindex.SearchIndex
	docScan       documentstore.StoredDocuments
	indexAdmin    *indexAdminController
	postings      rwi.PostingIndex
	urlDirectory  urlmeta.URLDirectory
	roster        peerroster.Roster
	news          *peernews.Pool
	sweeper       eviction.Sweeper
	announcer     peerannouncement.Announcer
	crawl         crawlProcess
	dht           dhtOutboundProcess
	vault         *vault.Vault
	client        *http.Client
	peerBlock     *peerblock.Store
	identity      nodeidentity.Identity
}

type nodeTelemetry struct {
	dhtOutbound      dhtexchange.DistributionObserver
	dhtInbound       *metrics.DHTInboundMetrics
	peer             *metrics.PeerMetrics
	search           *metrics.SearchMetrics
	crawl            *metrics.CrawlMetrics
	crawlRuns        *metrics.CrawlRunMetrics
	recorder         *events.Recorder
	searchAuthorizer tavilyapi.ScopeAuthorizer
	toggles          *runtimeToggles
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

	roster, news, tally, blocks, err := openPeerStores(vault, telemetry.peer)
	if err != nil {
		return node{}, err
	}

	report := newNodeStatusReport(identity, storage, roster, news, tally)
	storage = observeDHTInboundStorage(storage, telemetry.dhtInbound, tally)

	mux := http.NewServeMux()
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
		toggles:   telemetry.toggles,
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
		report:    report,
		searcher:  surfaces.searcher,
		roster:    roster,
		news:      news,
		vault:     vault,
		client:    client,
		peerBlock: blocks,
		identity:  identity,
		ranking:   surfaces.ranking,
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
	toggles   *runtimeToggles
}

type nodeSurfaces struct {
	crawl    crawlProcess
	dht      dhtOutboundProcess
	searcher searchcore.Searcher
	ranking  *rankingprofile.Holder
}

func assembleNodeSurfaces(in assembleSurfacesInput) (nodeSurfaces, error) {
	runtime, err := buildRuntimeCrawl(in.config.Crawl, in.identity, in.storage, in.vault)
	if err != nil {
		return nodeSurfaces{}, err
	}
	attachCrawlMetrics(runtime, in.telemetry.crawl)
	attachCrawlRunObserver(runtime, in.telemetry.crawlRuns, in.telemetry.recorder)
	ranking, err := rankingprofile.Open(in.ctx, in.vault)
	if err != nil {
		return nodeSurfaces{}, fmt.Errorf("open ranking profile: %w", err)
	}
	searcher := mountNodePublicSearch(in.mux, publicSearchAssembly{
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
		toggles:          in.toggles,
		queryLogMode:     in.config.QueryLogMode,
		searchMetrics:    in.telemetry.search,
		rankingWeights:   ranking.Current,
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

	return nodeSurfaces{crawl: runtime, dht: dht, searcher: searcher, ranking: ranking}, nil
}

type nodeParts struct {
	mux       *http.ServeMux
	storage   nodeStorage
	announcer peerannouncement.Announcer
	crawl     crawlProcess
	dht       dhtOutboundProcess
	report    nodestatus.Report
	searcher  searchcore.Searcher
	roster    peerroster.Roster
	news      *peernews.Pool
	vault     *vault.Vault
	client    *http.Client
	peerBlock *peerblock.Store
	identity  nodeidentity.Identity
	ranking   *rankingprofile.Holder
}

func newAssembledNode(parts nodeParts) node {
	return node{
		peerMux:       parts.mux,
		readiness:     newReadinessEndpoint(parts.storage.searchIndex),
		indexStats:    newIndexStatsEndpoint(parts.storage.searchIndex),
		searchExplain: newSearchExplainEndpoint(parts.storage.searchIndex),
		searchRanking: newSearchRankingEndpoint(parts.ranking),
		report:        parts.report,
		searcher:      parts.searcher,
		index:         parts.storage.searchIndex,
		docScan:       parts.storage.storedDocuments(),
		indexAdmin:    newIndexAdminController(parts.storage, parts.vault),
		postings:      parts.storage.postings,
		urlDirectory:  parts.storage.urlDirectory,
		roster:        parts.roster,
		news:          parts.news,
		sweeper:       newStorageSweeper(parts.vault, parts.storage),
		announcer:     parts.announcer,
		crawl:         parts.crawl,
		dht:           parts.dht,
		vault:         parts.vault,
		client:        parts.client,
		peerBlock:     parts.peerBlock,
		identity:      parts.identity,
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
