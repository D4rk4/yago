package yagonode

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/clickcapture"
	"github.com/D4rk4/yago/yagonode/internal/crawling"
	"github.com/D4rk4/yago/yagonode/internal/crawlschedule"
	"github.com/D4rk4/yago/yagonode/internal/crawlurls"
	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
	"github.com/D4rk4/yago/yagonode/internal/documentsearch"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/eviction"
	"github.com/D4rk4/yago/yagonode/internal/hostlinks"
	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/hosttrust"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/judgments"
	"github.com/D4rk4/yago/yagonode/internal/landiscovery"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/nodestatus"
	"github.com/D4rk4/yago/yagonode/internal/peerannouncement"
	"github.com/D4rk4/yago/yagonode/internal/peerbirth"
	"github.com/D4rk4/yago/yagonode/internal/peerblock"
	"github.com/D4rk4/yago/yagonode/internal/peernews"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
	"github.com/D4rk4/yago/yagonode/internal/portaltheme"
	"github.com/D4rk4/yago/yagonode/internal/publicratelimit"
	"github.com/D4rk4/yago/yagonode/internal/rankingmodel"
	"github.com/D4rk4/yago/yagonode/internal/rankingprofile"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/safetymodel"
	"github.com/D4rk4/yago/yagonode/internal/searchactivity"
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
	peerMux        *http.ServeMux
	publicMux      http.Handler
	readiness      http.Handler
	indexStats     http.Handler
	searchExplain  http.Handler
	searchRanking  http.Handler
	searchTune     http.Handler
	searchModel    http.Handler
	searchTrain    http.Handler
	searchRollback http.Handler
	safetyModel    http.Handler
	safetyTrain    http.Handler
	safetyRollback http.Handler
	judgmentsAPI   http.Handler
	hostTrustAPI   http.Handler
	rankingConsole adminui.RankingSource
	report         nodestatus.Report
	searcher       searchcore.Searcher
	suggest        searchcore.Searcher
	index          searchindex.SearchIndex
	docScan        documentstore.StoredDocuments
	redirectPurge  redirectCorpusPurge
	activity       *searchactivity.Tracker
	schedules      *crawlschedule.Store
	hostRank       *hostrank.Holder
	hostTrust      *hosttrust.Catalog
	spell          *spellcheck.Holder
	wordForms      *wordforms.Holder
	swarmMorph     bool
	corpusPass     *corpusSignalRefresh
	indexAdmin     *indexAdminController
	postings       rwi.PostingIndex
	urlDirectory   urlmeta.URLDirectory
	roster         peerroster.Roster
	news           *peernews.Pool
	sweeper        eviction.Sweeper
	announcer      peerannouncement.Announcer
	lanBeacon      *landiscovery.Beacon
	crawl          crawlProcess
	dht            dhtOutboundProcess
	vault          *vault.Vault
	toggles        *runtimeToggles
	client         *http.Client
	peerBlock      *peerblock.Store
	denylist       *urldenylist.Store
	clicks         *clickcapture.Store
	identity       nodeidentity.Identity
	theme          *portaltheme.Theme
	peerEvents     *peerReputationObserver
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

// newNodeWireMux builds the peer-protocol mux and its guarded wire router, and
// mounts the peer landing page on the mux root, keeping assembleNode within its
// length budget. The landing's exact-match "/{$}" route is order-independent of
// the /yacy/* wire handlers mounted afterwards.
func newNodeWireMux(
	config nodeConfig,
	report nodestatus.Report,
) (*http.ServeMux, httpguard.WireRouter) {
	mux := http.NewServeMux()
	mountPeerLanding(mux)

	return mux, httpguard.NewWireRouter(mux, newRuntimeWireGate(config, report))
}

func assembleNode(
	ctx context.Context,
	config nodeConfig,
	vault *vault.Vault,
	client *http.Client,
	telemetry nodeTelemetry,
) (node, error) {
	identity, storage, err := openNodeCore(ctx, config, vault)
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
	mountNodeWireHandlers(router, identity, storage, telemetry.saturation, config)
	peerClient := nodePeerClient(config, client)
	hostLinkSnapshot := hostlinks.NewSnapshotHolder()
	exchange, err := assembleRuntimePeerExchange(peerExchange{
		router:   router,
		identity: identity,
		report:   report,
		config:   config,
		vault:    vault,
		client:   peerClient,
		peer:     telemetry.peer,
		host:     hostLinkSnapshot,
		roster:   roster,
		news:     news,
	})
	if err != nil {
		return node{}, err
	}
	surfaces, err := assembleNodeSurfaces(assembleSurfacesInput{
		ctx: ctx, config: config, vault: vault, client: client,
		peerClient: peerClient, storage: storage, roster: roster, identity: identity,
		report: report, tally: tally, telemetry: telemetry, toggles: telemetry.toggles,
		hostLinks: hostLinkSnapshot,
	})
	if err != nil {
		return node{}, err
	}
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
		activity:   surfaces.activity,
		schedules:  surfaces.schedules,
		theme:      surfaces.theme,
		identity:   identity,
		ranking:    surfaces.ranking,
		hostRank:   surfaces.hostRank,
		spell:      surfaces.spell,
		wordForms:  surfaces.wordForms,
		judgments:  surfaces.judgments,
		clicks:     surfaces.clicks,
		models:     surfaces.models,
		safety:     surfaces.safety,
		hostTrust:  surfaces.trust,
		peerEvents: surfaces.peerEvents,
		corpusPass: surfaces.corpusPass,
		swarmMorph: config.SwarmMorphology,
	}, telemetry.toggles), nil
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
	hostLinks  *hostlinks.SnapshotHolder
}

type nodeSurfaces struct {
	crawl      crawlProcess
	dht        dhtOutboundProcess
	searcher   searchcore.Searcher
	suggest    searchcore.Searcher
	publicMux  http.Handler
	ranking    *rankingprofile.Holder
	hostRank   *hostrank.Holder
	spell      *spellcheck.Holder
	wordForms  *wordforms.Holder
	denylist   *urldenylist.Store
	judgments  *judgments.Store
	clicks     *clickcapture.Store
	models     *rankingmodel.Catalog
	safety     *safetymodel.Catalog
	trust      *hosttrust.Catalog
	activity   *searchactivity.Tracker
	schedules  *crawlschedule.Store
	theme      *portaltheme.Theme
	peerEvents *peerReputationObserver
	corpusPass *corpusSignalRefresh
}

func assembleNodeSurfaces(in assembleSurfacesInput) (nodeSurfaces, error) {
	runtime, err := buildRuntimeCrawl(in.config.Crawl, in.identity, in.storage, in.vault)
	if err != nil {
		return nodeSurfaces{}, err
	}
	attachCrawlMetrics(runtime, in.telemetry.crawl)
	attachCrawlRunObserver(runtime, in.telemetry.crawlRuns, in.telemetry.recorder)
	ranking, denylist, schedules, err := openSurfaceStores(in.ctx, in.vault)
	if err != nil {
		return nodeSurfaces{}, err
	}
	learning, err := openSearchLearningStores(in.ctx, in.vault)
	if err != nil {
		return nodeSurfaces{}, err
	}
	attachContentSafetyClassifier(runtime, learning.safety)
	theme, err := portaltheme.Open(in.vault, themeEventSink(in.telemetry.recorder))
	if err != nil {
		learning.peerEvents.Close()
		return nodeSurfaces{}, fmt.Errorf("open portal theme: %w", err)
	}
	publicMux := http.NewServeMux()
	searchLimiter := publicratelimit.NewLimiter(searchRateTiers(in.config.SearchRate))
	activityTracker := searchactivity.New(searchactivity.Mode(in.config.QueryLogMode))
	corpusSignals := newCorpusSignalSet(in, learning)
	searcher, suggest := mountNodePublicSearch(publicMux, newPublicSearchAssembly(
		in,
		publicSearchParts{
			runtime:    runtime,
			ranking:    ranking,
			denylist:   denylist,
			activity:   activityTracker,
			hostRank:   corpusSignals.authority,
			spell:      corpusSignals.spelling,
			words:      corpusSignals.wordForms,
			theme:      theme,
			clicks:     learning.clicks,
			models:     learning.models,
			reputation: learning.reputation,
			peerEvents: learning.peerEvents,
			admission:  newTavilySearchAdmission(searchLimiter),
		},
	))
	dht := buildSurfaceDHT(in, runtime)
	searchAccess := searchAccessPolicy(publicSearchAssembly{
		searchAuthorizer: in.telemetry.searchAuthorizer,
		searchAPIKey:     in.config.SearchAPIKey,
	})
	limitedPublic := publicratelimit.Wrap(publicMux, searchLimiter, searchAccess.AuthenticatedRead)
	return nodeSurfaces{
		crawl:      runtime,
		dht:        dht,
		searcher:   searcher,
		suggest:    suggest,
		publicMux:  limitedPublic,
		theme:      theme,
		ranking:    ranking,
		hostRank:   corpusSignals.authority,
		spell:      corpusSignals.spelling,
		wordForms:  corpusSignals.wordForms,
		denylist:   denylist,
		activity:   activityTracker,
		schedules:  schedules,
		judgments:  learning.judgments,
		clicks:     learning.clicks,
		models:     learning.models,
		safety:     learning.safety,
		trust:      learning.trust,
		peerEvents: learning.peerEvents,
		corpusPass: corpusSignals.refresh,
	}, nil
}

// publicSearchParts carries the surface stores and runtimes assembleNodeSurfaces
// opened into the public-search assembly literal, keeping the function inside
// its length budget.
type publicSearchParts struct {
	runtime    crawlProcess
	ranking    *rankingprofile.Holder
	denylist   *urldenylist.Store
	activity   *searchactivity.Tracker
	hostRank   *hostrank.Holder
	spell      *spellcheck.Holder
	words      *wordforms.Holder
	theme      *portaltheme.Theme
	clicks     *clickcapture.Store
	models     *rankingmodel.Catalog
	reputation *peerReputationObserver
	peerEvents *peerReputationObserver
	admission  tavilyapi.SearchAdmission
}

func newPublicSearchAssembly(
	in assembleSurfacesInput,
	parts publicSearchParts,
) publicSearchAssembly {
	return publicSearchAssembly{
		storage:             in.storage,
		hostRank:            parts.hostRank.Current,
		spellCorrector:      parts.spell.Current,
		wordForms:           parts.words.Current,
		roster:              in.roster,
		identity:            in.identity,
		dht:                 in.config.DHT,
		client:              in.client,
		peerClient:          in.peerClient,
		peerHTTPSPreferred:  in.config.PeerHTTPSPreferred,
		searchAPIKey:        in.config.SearchAPIKey,
		searchAuthorizer:    in.telemetry.searchAuthorizer,
		searchAdmission:     parts.admission,
		extractFetcher:      buildExtractFetcher(in.config, in.client),
		webFallback:         in.config.WebFallback,
		seedQueue:           crawlOrderQueue(parts.runtime),
		toggles:             in.toggles,
		queryLogMode:        in.config.QueryLogMode,
		activity:            parts.activity,
		searchMetrics:       in.telemetry.search,
		rankingWeights:      parts.ranking.Current,
		denylist:            parts.denylist,
		snippetEnricher:     buildSnippetEnricher(in.config, in.client),
		remoteTimeouts:      configRemoteTimeouts(in.config),
		indexRemoteResults:  in.config.IndexRemoteResults,
		swarmMorphology:     in.config.SwarmMorphology,
		swarmSeed:           in.config.SwarmSeed,
		autocrawlerCrawl:    in.config.AutocrawlerCrawl,
		linksNewTab:         in.config.SearchLinksNewTab,
		clickCapture:        in.config.SearchClickCapture,
		clickRecorder:       newClickCaptureAdapter(parts.clicks, parts.models.Ranker()),
		portalClickRecorder: newPortalClickCaptureAdapter(parts.clicks, parts.models.Ranker()),
		learnedRanker:       parts.models.Ranker(),
		peerReputation:      parts.reputation,
		peerObservations:    parts.peerEvents,
		peerNetworkGroup:    peerReputationNetworkGroup,
		selfSeed:            in.report.SelfSeed,
		theme:               parts.theme,
	}
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
	activity   *searchactivity.Tracker
	schedules  *crawlschedule.Store
	identity   nodeidentity.Identity
	ranking    *rankingprofile.Holder
	hostRank   *hostrank.Holder
	hostTrust  *hosttrust.Catalog
	spell      *spellcheck.Holder
	wordForms  *wordforms.Holder
	judgments  *judgments.Store
	clicks     *clickcapture.Store
	models     *rankingmodel.Catalog
	safety     *safetymodel.Catalog
	swarmMorph bool
	theme      *portaltheme.Theme
	peerEvents *peerReputationObserver
	corpusPass *corpusSignalRefresh
}

func newAssembledNode(parts nodeParts, toggles *runtimeToggles) node {
	rankingRuntime := newNodeRankingRuntime(parts)
	return node{
		peerMux:    parts.mux,
		publicMux:  parts.publicMux,
		readiness:  newReadinessEndpoint(parts.storage.searchIndex),
		indexStats: newIndexStatsEndpoint(parts.storage.searchIndex),
		searchExplain: newSearchExplainEndpoint(
			parts.storage.searchIndex,
			parts.ranking.Current,
			parts.hostRank.Current,
			parts.models.Ranker(),
			parts.denylist,
		),
		searchRanking:  newSearchRankingEndpoint(parts.ranking),
		searchTune:     newSearchRankingTuneEndpoint(rankingRuntime.tuner),
		searchModel:    newSearchRankingModelEndpoint(parts.models),
		searchTrain:    newSearchRankingTrainEndpoint(rankingRuntime.trainer),
		searchRollback: newSearchRankingRollbackEndpoint(parts.models),
		safetyModel:    newSearchSafetyModelEndpoint(parts.safety),
		safetyTrain:    newSearchSafetyTrainEndpoint(parts.safety),
		safetyRollback: newSearchSafetyRollbackEndpoint(parts.safety),
		judgmentsAPI:   newSearchJudgmentsEndpoint(parts.judgments),
		hostTrustAPI:   newSearchHostTrustEndpoint(parts.hostTrust),
		rankingConsole: newRankingConsole(
			parts.ranking,
			rankingRuntime.tuner,
			parts.judgments,
			rankingConsoleLearning{
				trainer: rankingRuntime.trainer, models: parts.models, trust: parts.hostTrust,
			},
		),
		report:        parts.report,
		searcher:      parts.searcher,
		suggest:       parts.suggest,
		index:         parts.storage.searchIndex,
		docScan:       parts.storage.storedDocuments(),
		redirectPurge: newNodeRedirectPurge(parts.storage, parts.vault),
		activity:      parts.activity,
		schedules:     parts.schedules,
		hostRank:      parts.hostRank,
		hostTrust:     parts.hostTrust,
		spell:         parts.spell,
		wordForms:     parts.wordForms,
		swarmMorph:    parts.swarmMorph,
		corpusPass:    parts.corpusPass,
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
		toggles:       toggles,
		client:        parts.client,
		peerBlock:     parts.peerBlock,
		denylist:      parts.denylist,
		clicks:        parts.clicks,
		identity:      parts.identity,
		theme:         parts.theme,
		peerEvents:    parts.peerEvents,
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
		Intake:  httpguard.NewIntakeGate(maximumConcurrentWireRequests),
	}
}

// mountNodeWireHandlers registers the YaCy wire-protocol routes and the
// crawl-compatibility routes on the peer router in one step. acceptRemoteIndex
// mirrors the advertised capability into the DHT-in transfer endpoints.
func mountNodeWireHandlers(
	router httpguard.WireRouter,
	identity nodeidentity.Identity,
	storage nodeStorage,
	saturation *metrics.SaturationMetrics,
	config nodeConfig,
) {
	mountNodeProtocol(router, identity, storage, saturation, config.AdvertiseRemoteIndex)
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
	acceptRemoteIndex bool,
) {
	// One admission gate covers both DHT-in transfer endpoints (YaCy 1.6
	// load-limits the whole DHT intake), a separate one bounds concurrent
	// inbound remote searches (YaCy 1.0 distributed-search DoS protection).
	// Each shed request counts as a saturation event (USE method, OPS-07).
	transferGate := httpguard.NewObservedIntakeGate(
		dhtInboundTransferSlots,
		saturation.RejectionObserver(metrics.GateDHTTransfer),
	)
	urlmeta.MountTransferURL(
		router, identity, storage.urlReceiver, transferGate, acceptRemoteIndex,
	)
	rwi.MountTransferRWI(
		router,
		identity,
		storage.postingReceiver,
		transferGate,
		rwi.Config{
			BatchCap:          receiveBatchCap,
			PauseSeconds:      receiveBusyPauseSecs,
			AcceptRemoteIndex: acceptRemoteIndex,
		},
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
		storage.documentEvictor(),
		storage.urlDirectory,
		storage.staleness,
		eviction.Config{TargetFraction: evictionTargetFraction, BatchSize: evictionBatch},
	)
}

// openNodeCore resolves the node identity and opens the storage stack — the
// first, order-sensitive steps of assembly.
func openNodeCore(
	ctx context.Context,
	config nodeConfig,
	vault *vault.Vault,
) (nodeidentity.Identity, nodeStorage, error) {
	identity, err := nodeIdentityWithBirthDate(ctx, config, vault)
	if err != nil {
		return nodeidentity.Identity{}, nodeStorage{}, err
	}
	storage, err := openRuntimeNodeStorage(vault, config.SearchIndexPath)
	if err != nil {
		return nodeidentity.Identity{}, nodeStorage{}, err
	}

	return identity, storage, nil
}

// openSurfaceStores opens the vault-backed stores the public surfaces and
// the crawl scheduler need.
func openSurfaceStores(
	ctx context.Context,
	vaultStore *vault.Vault,
) (*rankingprofile.Holder, *urldenylist.Store, *crawlschedule.Store, error) {
	ranking, err := rankingprofile.Open(ctx, vaultStore)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open ranking profile: %w", err)
	}
	denylist, err := urldenylist.Open(vaultStore, time.Now)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open url denylist: %w", err)
	}
	schedules, err := crawlschedule.Open(vaultStore, time.Now)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open crawl schedules: %w", err)
	}

	return ranking, denylist, schedules, nil
}
