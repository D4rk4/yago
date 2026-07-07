package yagonode

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/crawling"
	"github.com/D4rk4/yago/yagonode/internal/crawlurls"
	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
	"github.com/D4rk4/yago/yagonode/internal/documentsearch"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/eviction"
	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/landiscovery"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/nodestatus"
	"github.com/D4rk4/yago/yagonode/internal/peerannouncement"
	"github.com/D4rk4/yago/yagonode/internal/peerbirth"
	"github.com/D4rk4/yago/yagonode/internal/peerblock"
	"github.com/D4rk4/yago/yagonode/internal/peernews"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
	"github.com/D4rk4/yago/yagonode/internal/publicratelimit"
	"github.com/D4rk4/yago/yagonode/internal/rankingprofile"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/spellcheck"
	"github.com/D4rk4/yago/yagonode/internal/tavilyapi"
	"github.com/D4rk4/yago/yagonode/internal/transfertally"
	"github.com/D4rk4/yago/yagonode/internal/urldenylist"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
	"github.com/D4rk4/yago/yagonode/internal/vault"
	"github.com/D4rk4/yago/yagonode/internal/wordforms"
)

type node struct {
	peerMux       *http.ServeMux
	publicMux     http.Handler
	readiness     http.Handler
	indexStats    http.Handler
	searchExplain http.Handler
	searchRanking http.Handler
	report        nodestatus.Report
	searcher      searchcore.Searcher
	suggest       searchcore.Searcher
	index         searchindex.SearchIndex
	docScan       documentstore.StoredDocuments
	docEvictor    documentstore.DocumentEvictor
	hostRank      *hostrank.Holder
	spell         *spellcheck.Holder
	wordForms     *wordforms.Holder
	swarmMorph    bool
	indexAdmin    *indexAdminController
	postings      rwi.PostingIndex
	urlDirectory  urlmeta.URLDirectory
	roster        peerroster.Roster
	news          *peernews.Pool
	sweeper       eviction.Sweeper
	announcer     peerannouncement.Announcer
	lanBeacon     *landiscovery.Beacon
	crawl         crawlProcess
	dht           dhtOutboundProcess
	vault         *vault.Vault
	client        *http.Client
	peerBlock     *peerblock.Store
	denylist      *urldenylist.Store
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
	saturation       *metrics.SaturationMetrics
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

// newNodeWireMux builds the peer-protocol mux and its guarded wire router,
// keeping assembleNode within its length budget.
func newNodeWireMux(
	config nodeConfig,
	report nodestatus.Report,
) (*http.ServeMux, httpguard.WireRouter) {
	mux := http.NewServeMux()

	return mux, httpguard.NewWireRouter(mux, newRuntimeWireGate(config, report))
}

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
	mux, router := newNodeWireMux(config, report)
	mountNodeWireHandlers(router, identity, storage, telemetry.saturation)
	peerClient := nodePeerClient(config, client)
	exchange, err := assembleRuntimePeerExchange(peerExchange{
		router:   router,
		identity: identity,
		report:   report,
		config:   config,
		vault:    vault,
		client:   peerClient,
		peer:     telemetry.peer,
		host:     storedDocumentHostLinks{documents: storage.storedDocuments()},
		roster:   roster,
		news:     news,
	})
	if err != nil {
		return node{}, err
	}
	surfaces, err := assembleNodeSurfaces(assembleSurfacesInput{
		ctx:        ctx,
		config:     config,
		vault:      vault,
		client:     client,
		peerClient: peerClient,
		storage:    storage,
		roster:     roster,
		identity:   identity,
		report:     report,
		tally:      tally,
		telemetry:  telemetry,
		toggles:    telemetry.toggles,
	})
	if err != nil {
		return node{}, err
	}
	mountPeerLanding(mux)
	return newAssembledNode(nodeParts{
		mux:        mux,
		publicMux:  surfaces.publicMux,
		storage:    storage,
		announcer:  exchange.announcer,
		lanBeacon:  buildLANBeacon(config, identity, exchange.announcer),
		crawl:      surfaces.crawl,
		dht:        surfaces.dht,
		report:     report,
		searcher:   surfaces.searcher,
		suggest:    surfaces.suggest,
		roster:     roster,
		news:       news,
		vault:      vault,
		client:     client,
		peerBlock:  blocks,
		denylist:   surfaces.denylist,
		identity:   identity,
		ranking:    surfaces.ranking,
		hostRank:   surfaces.hostRank,
		spell:      surfaces.spell,
		wordForms:  surfaces.wordForms,
		swarmMorph: config.SwarmMorphology,
	}), nil
}

// nodePeerClient picks the client for peer-protocol calls: a client tolerant
// of the self-signed certificates YaCy peers serve when https preference is
// on, and the plain egress client verbatim otherwise.
func nodePeerClient(config nodeConfig, client *http.Client) *http.Client {
	if config.PeerHTTPSPreferred {
		return newRuntimePeerProtocolClient(config)
	}

	return client
}

type assembleSurfacesInput struct {
	ctx        context.Context
	config     nodeConfig
	vault      *vault.Vault
	client     *http.Client
	peerClient *http.Client
	storage    nodeStorage
	roster     peerroster.Roster
	identity   nodeidentity.Identity
	report     nodestatus.Report
	tally      *transfertally.Tally
	telemetry  nodeTelemetry
	toggles    *runtimeToggles
}

type nodeSurfaces struct {
	crawl     crawlProcess
	dht       dhtOutboundProcess
	searcher  searchcore.Searcher
	suggest   searchcore.Searcher
	publicMux http.Handler
	ranking   *rankingprofile.Holder
	hostRank  *hostrank.Holder
	spell     *spellcheck.Holder
	wordForms *wordforms.Holder
	denylist  *urldenylist.Store
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
	denylist, err := urldenylist.Open(in.vault, time.Now)
	if err != nil {
		return nodeSurfaces{}, fmt.Errorf("open url denylist: %w", err)
	}
	publicMux := http.NewServeMux()
	hostRankHolder := hostrank.NewHolder()
	spellHolder := spellcheck.NewHolder()
	wordFormsHolder := wordforms.NewHolder()
	searcher, suggest := mountNodePublicSearch(publicMux, publicSearchAssembly{
		storage:            in.storage,
		hostRank:           hostRankHolder.Current,
		spellCorrector:     spellHolder.Current,
		wordForms:          wordFormsHolder.Current,
		roster:             in.roster,
		identity:           in.identity,
		dht:                in.config.DHT,
		client:             in.client,
		peerClient:         in.peerClient,
		peerHTTPSPreferred: in.config.PeerHTTPSPreferred,
		searchAPIKey:       in.config.SearchAPIKey,
		searchAuthorizer:   in.telemetry.searchAuthorizer,
		extractFetcher:     buildExtractFetcher(in.config, in.client),
		webFallback:        in.config.WebFallback,
		seedQueue:          crawlOrderQueue(runtime),
		toggles:            in.toggles,
		queryLogMode:       in.config.QueryLogMode,
		searchMetrics:      in.telemetry.search,
		rankingWeights:     ranking.Current,
		denylist:           denylist,
		snippetEnricher:    buildSnippetEnricher(in.config, in.client),
		remoteTimeouts:     configRemoteTimeouts(in.config),
		indexRemoteResults: in.config.IndexRemoteResults,
		swarmMorphology:    in.config.SwarmMorphology,
		swarmSeed:          in.config.SwarmSeed,
		linksNewTab:        in.config.SearchLinksNewTab,
	})
	dht := buildRuntimeDHTOutbound(dhtOutboundRuntimeAssembly{
		ctx:         in.ctx,
		config:      in.config,
		storage:     in.vault,
		nodeStorage: in.storage,
		report:      in.report,
		roster:      in.roster,
		client:      in.peerClient,
		observer:    tallyOutboundObserver{next: in.telemetry.dhtOutbound, tally: in.tally},
	})

	// The public search paths throttle per client (YaCy search.public.max.access
	// tiers); authenticated keys and the local operator get raised limits.
	limitedPublic := publicratelimit.Wrap(
		publicMux,
		publicratelimit.NewLimiter(publicratelimit.DefaultPublicTiers()),
		searchAccessPolicy(publicSearchAssembly{
			searchAuthorizer: in.telemetry.searchAuthorizer,
			searchAPIKey:     in.config.SearchAPIKey,
		}).AuthenticatedRead,
	)

	return nodeSurfaces{
		crawl:     runtime,
		dht:       dht,
		searcher:  searcher,
		suggest:   suggest,
		publicMux: limitedPublic,
		ranking:   ranking,
		hostRank:  hostRankHolder,
		spell:     spellHolder,
		wordForms: wordFormsHolder,
		denylist:  denylist,
	}, nil
}

type nodeParts struct {
	mux        *http.ServeMux
	publicMux  http.Handler
	storage    nodeStorage
	announcer  peerannouncement.Announcer
	lanBeacon  *landiscovery.Beacon
	crawl      crawlProcess
	dht        dhtOutboundProcess
	report     nodestatus.Report
	searcher   searchcore.Searcher
	suggest    searchcore.Searcher
	roster     peerroster.Roster
	news       *peernews.Pool
	vault      *vault.Vault
	client     *http.Client
	peerBlock  *peerblock.Store
	denylist   *urldenylist.Store
	identity   nodeidentity.Identity
	ranking    *rankingprofile.Holder
	hostRank   *hostrank.Holder
	spell      *spellcheck.Holder
	wordForms  *wordforms.Holder
	swarmMorph bool
}

func newAssembledNode(parts nodeParts) node {
	return node{
		peerMux:       parts.mux,
		publicMux:     parts.publicMux,
		readiness:     newReadinessEndpoint(parts.storage.searchIndex),
		indexStats:    newIndexStatsEndpoint(parts.storage.searchIndex),
		searchExplain: newSearchExplainEndpoint(parts.storage.searchIndex),
		searchRanking: newSearchRankingEndpoint(parts.ranking),
		report:        parts.report,
		searcher:      parts.searcher,
		suggest:       parts.suggest,
		index:         parts.storage.searchIndex,
		docScan:       parts.storage.storedDocuments(),
		docEvictor:    documentEvictorOf(parts.storage),
		hostRank:      parts.hostRank,
		spell:         parts.spell,
		wordForms:     parts.wordForms,
		swarmMorph:    parts.swarmMorph,
		indexAdmin:    newIndexAdminController(parts.storage, parts.vault),
		postings:      parts.storage.postings,
		urlDirectory:  parts.storage.urlDirectory,
		roster:        parts.roster,
		news:          parts.news,
		sweeper:       newStorageSweeper(parts.vault, parts.storage),
		announcer:     parts.announcer,
		lanBeacon:     parts.lanBeacon,
		crawl:         parts.crawl,
		dht:           parts.dht,
		vault:         parts.vault,
		client:        parts.client,
		peerBlock:     parts.peerBlock,
		denylist:      parts.denylist,
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

// mountNodeWireHandlers registers the YaCy wire-protocol routes and the
// crawl-compatibility routes on the peer router in one step.
func mountNodeWireHandlers(
	router httpguard.WireRouter,
	identity nodeidentity.Identity,
	storage nodeStorage,
	saturation *metrics.SaturationMetrics,
) {
	mountNodeProtocol(router, identity, storage, saturation)
	mountNodeCrawlCompatibility(router, identity, storage)
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
	saturation *metrics.SaturationMetrics,
) {
	// One admission gate covers both DHT-in transfer endpoints (YaCy 1.6
	// load-limits the whole DHT intake), a separate one bounds concurrent
	// inbound remote searches (YaCy 1.0 distributed-search DoS protection).
	// Each shed request counts as a saturation event (USE method, OPS-07).
	transferGate := httpguard.NewObservedIntakeGate(
		dhtInboundTransferSlots,
		saturation.RejectionObserver(metrics.GateDHTTransfer),
	)
	urlmeta.MountTransferURL(router, identity, storage.urlReceiver, transferGate)
	rwi.MountTransferRWI(
		router,
		identity,
		storage.postingReceiver,
		transferGate,
		rwi.Config{BatchCap: receiveBatchCap, PauseSeconds: receiveBusyPauseSecs},
	)
	nodestatus.MountQuery(
		router,
		identity,
		storage.postings,
		storage.urlDirectory,
	)
	documentsearch.MountSearch(router, identity, documentsearch.SearchConfig{
		Index:          storage.postings,
		Documents:      storage.urlDirectory,
		MatchesPerTerm: searchPostingsPerWord,
		Gate: httpguard.NewObservedIntakeGate(
			inboundRemoteSearchSlots,
			saturation.RejectionObserver(metrics.GateRemoteSearch),
		),
	})
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
