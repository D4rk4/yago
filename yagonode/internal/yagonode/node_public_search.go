package yagonode

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/cachedpage"
	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
	"github.com/D4rk4/yago/yagonode/internal/documentsearch"
	"github.com/D4rk4/yago/yagonode/internal/faviconproxy"
	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/landing"
	"github.com/D4rk4/yago/yagonode/internal/learnedrank"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
	"github.com/D4rk4/yago/yagonode/internal/portaltheme"
	"github.com/D4rk4/yago/yagonode/internal/publicportal"
	"github.com/D4rk4/yago/yagonode/internal/publicrobots"
	"github.com/D4rk4/yago/yagonode/internal/searchactivity"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/searchlocal"
	"github.com/D4rk4/yago/yagonode/internal/searchremote"
	"github.com/D4rk4/yago/yagonode/internal/searchsession"
	"github.com/D4rk4/yago/yagonode/internal/siteicon"
	"github.com/D4rk4/yago/yagonode/internal/snippetfetch"
	"github.com/D4rk4/yago/yagonode/internal/spellcheck"
	"github.com/D4rk4/yago/yagonode/internal/tavilyapi"
	"github.com/D4rk4/yago/yagonode/internal/websearch"
	"github.com/D4rk4/yago/yagonode/internal/wordforms"
	"github.com/D4rk4/yago/yagonode/internal/yacysearch"
)

type publicSearchAssembly struct {
	storage                nodeStorage
	roster                 peerroster.Roster
	identity               nodeidentity.Identity
	dht                    dhtDistributionConfig
	client                 *http.Client
	peerClient             *http.Client
	peerHTTPSPreferred     bool
	dhtSearchTargetIndex   func(int) (int, error)
	searchAPIKey           string
	searchAuthorizer       tavilyapi.ScopeAuthorizer
	searchAdmission        tavilyapi.SearchAdmission
	extractFetcher         tavilyapi.ContentFetcher
	webFallback            webFallbackConfig
	seedQueue              crawldispatch.CrawlOrderQueue
	maxPagesPerRun         func() int
	toggles                *runtimeToggles
	queryLogMode           queryLogMode
	activity               *searchactivity.Tracker
	searchMetrics          *metrics.SearchMetrics
	rankingWeights         func() searchindex.RankingWeights
	hostRank               func() hostrank.AuthorityTable
	spellCorrector         func() *spellcheck.Corrector
	denylist               denylistSnapshotter
	snippetEnricher        *snippetfetch.Enricher
	remoteTimeouts         remoteSearchTimeouts
	indexRemoteResults     bool
	storageGrowth          growthAdmission
	swarmMorphology        bool
	wordForms              func() *wordforms.Expander
	swarmSeed              swarmSeedConfig
	autocrawlerCrawl       seedCrawlOptions
	linksNewTab            bool
	clickCapture           bool
	clickRecorder          yacysearch.ImpressionRecorder
	portalClickRecorder    publicportal.ImpressionRecorder
	learnedRanker          *learnedrank.Ranker
	peerReputation         searchremote.ReputationSnapshotSource
	peerObservations       searchremote.ReputationObservationSink
	peerReachability       searchremote.PeerReachability
	peerNetworkGroup       searchremote.ReputationNetworkGroup
	selfSeed               func(context.Context) yagomodel.Seed
	observeRemoteResources func(context.Context, int)
	// theme carries the operator portal theme (ADR-0033) into the portal
	// mount; nil keeps the built-in render only.
	theme *portaltheme.Theme
}

// remoteSearchTimeouts carries the swarm fan-out budgets into the assembly.
type remoteSearchTimeouts struct {
	perPeer time.Duration
	overall time.Duration
}

func configRemoteTimeouts(config nodeConfig) remoteSearchTimeouts {
	return remoteSearchTimeouts{
		perPeer: config.RemotePeerTimeout,
		overall: config.RemoteTimeout,
	}
}

// searchTargetPeers adapts the peer roster for remote-search target selection.
// DHT index distribution targets only greet-confirmed reachable peers, but YaCy
// selects remote-search targets from the seed database of known senior peers
// with no prior reachability handshake (net/yacy/peers/RemoteSearch.java ->
// DHTSelection.selectDHTSearchTargets). A node behind NAT — whose inbound hello,
// and thus reachability confirmation, never completes — must still search the
// network, so remote search draws from the known-peer set and lets unreachable
// candidates surface as per-peer partial failures rather than blocking outright.
type searchTargetPeers struct {
	roster peerroster.Roster
}

func (s searchTargetPeers) SearchTargetPeers(ctx context.Context) []yagomodel.Seed {
	if s.roster == nil {
		return nil
	}

	return s.roster.FreshestPeers(ctx, reservoirCapacity)
}

// parsedQuerySearcher fills a request's word-hash terms from its raw query when
// the caller supplied none. Human search surfaces (the admin console, the public
// portal) and API callers that pass only a query string would otherwise reach
// the remote DHT fan-out with no term hashes and get "no query terms"; the
// public /yacysearch and Tavily endpoints that already parse the query keep
// their terms untouched. It sits at the top of the shared searcher so every
// surface benefits.
type parsedQuerySearcher struct {
	inner searchcore.Searcher
}

func withParsedQuery(inner searchcore.Searcher) searchcore.Searcher {
	return parsedQuerySearcher{inner: inner}
}

func (s parsedQuerySearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	var err error
	req, err = searchcore.ParsePublicRequest(req)
	if err != nil {
		return searchcore.Response{}, fmt.Errorf("parse public search request: %w", err)
	}

	//nolint:wrapcheck // pass the wrapped searcher's error through unchanged.
	return s.inner.Search(ctx, req)
}

// remoteSearchClient picks the peer-protocol client for the remote search
// fan-out when one is wired, falling back to the general egress client.
func remoteSearchClient(assembly publicSearchAssembly) *http.Client {
	if assembly.peerClient != nil {
		return assembly.peerClient
	}

	return assembly.client
}

// remoteRankingWeights narrows the local ranking profile to the fields remote
// results can honor (title and URL text), so swarm hits are ranked by the
// same profile as local hits rather than by the sending peer's result order.
func remoteRankingWeights(
	current func() searchindex.RankingWeights,
) func() searchremote.RankingWeights {
	return func() searchremote.RankingWeights {
		if current == nil {
			return searchremote.DefaultRankingWeights()
		}
		weights := current()

		return searchremote.RankingWeights{Title: weights.Title, URL: weights.URL}
	}
}

// mountPeerLanding serves the static landing page at the peer listener's root so
// a human (or another peer) that opens the P2P port sees the node's identity. The
// peer listener otherwise carries only the /yacy/* wire protocol; the public
// search surfaces live on the separate public listener.
func mountPeerLanding(mux *http.ServeMux) {
	mux.Handle("/{$}", landing.NewEndpoint(buildVersion))
}

// mountNodePublicSearch wires the public surfaces and returns the decorated
// search pipeline plus the local-only suggest source (index suggestions with
// the denylist applied), which the admin console reuses for its autocomplete.
func mountNodePublicSearch(
	mux *http.ServeMux,
	assembly publicSearchAssembly,
) (searchcore.Searcher, searchcore.Searcher) {
	search, suggest, _ := mountNodePublicSearchWithExplanation(mux, assembly)

	return search, suggest
}

func mountNodePublicSearchWithExplanation(
	mux *http.ServeMux,
	assembly publicSearchAssembly,
) (searchcore.Searcher, searchcore.Searcher, searchcore.Searcher) {
	siteicon.Mount(mux)
	local := newLocalRankingSearcher(
		assembly.storage.searchIndex,
		assembly.rankingWeights,
		assembly.hostRank,
	)
	if assembly.storage.searchIndex == nil {
		local = documentsearch.NewLocalSearcherWithDocuments(
			assembly.storage.postings,
			assembly.storage.urlDirectory,
			assembly.storage.documentDirectory,
			searchPostingsPerWord,
		)
	}
	passages, _ := local.(searchcore.DocumentPassageSearcher)
	remoteConfig := publicRemoteSearchConfig(assembly)
	remote := searchremote.NewSearcher(remoteConfig)
	diagnosticRemote := searchremote.NewSearcher(publicDiagnosticRemoteSearchConfig(assembly))
	search := assemblePublicSearcher(local, remote, assembly)
	explanation := assemblePublicExplanationSearcher(local, diagnosticRemote, assembly)
	access := searchAccessPolicy(assembly)
	// Autocomplete suggestions come from the local index alone (denylist applied,
	// same as served results) so typeahead never fans out to the swarm or the web
	// fallback that the main search path can reach.
	suggestSource := withDenylistFilter(local, assembly.denylist)
	yacysearch.Mount(mux, search, suggestSource, assembly.linksNewTab, yacysearch.ClickCapture{
		Enabled:  assembly.clickCapture,
		Recorder: assembly.clickRecorder,
	})
	cachedpage.Mount(mux, assembly.storage.documentDirectory, passages)
	faviconproxy.Mount(mux, assembly.client)
	faviconproxy.MountImages(mux, assembly.client)
	tavilyapi.Mount(
		mux,
		search,
		assembly.storage.documentDirectory,
		access,
		assembly.searchAdmission,
	)
	tavilyapi.MountExtract(mux, assembly.storage.documentDirectory, access, assembly.extractFetcher)
	tavilyapi.MountCrawl(mux, access, crawlPageFetcher(assembly.extractFetcher))
	portal := publicportal.New(newPortalSource(search), assembly.linksNewTab)
	if assembly.clickCapture {
		portal.SetImpressionRecorder(assembly.portalClickRecorder)
	}
	if assembly.theme != nil {
		portal.SetTheme(assembly.theme)
	}
	mux.Handle("/{$}", newRootDispatcher(assembly.toggles, portal))
	publicportal.SetBaseURLProvider(assembly.toggles.PublicBaseURL)
	yacysearch.SetBaseURLProvider(assembly.toggles.PublicBaseURL)
	mountPortalOpenSearch(mux, assembly.toggles)
	publicrobots.Mount(mux, assembly.toggles.RobotsPolicy)
	publicportal.SetGreetingProvider(assembly.toggles.PortalGreeting)

	return search, suggestSource, explanation
}

// swarmMorphologyExpander builds the single-word expansion function for the
// remote searcher, or nil when swarm morphology is off or no expander is wired —
// so the exact conjunctive search stays the default.
func swarmMorphologyExpander(assembly publicSearchAssembly) func(string) []string {
	if !assembly.swarmMorphology || assembly.wordForms == nil {
		return nil
	}

	return func(word string) []string {
		observed := assembly.wordForms().Variants(word)
		generated := searchindex.GeneratedMorphologySurfaces(word)

		return prioritizedSwarmMorphologyForms(observed, generated)
	}
}

func assemblePublicSearcher(
	local searchcore.Searcher,
	remote searchcore.Searcher,
	assembly publicSearchAssembly,
) searchcore.Searcher {
	retrieval := assemblePublicRetrievalSearcher(local, remote, assembly)
	ranked := assembleRankingStages(retrieval, assembly)
	// The session cache makes paging stable (YaCy SearchEventCache): page one
	// runs one deep search, deeper pages slice the cached result list.
	stable := searchsession.NewStableWindow(ranked)
	pageEvidence := searchlocal.NewPageEvidenceSearcher(
		stable,
		searchEvidenceSource(assembly.storage.searchIndex),
	)
	search := pageEvidence
	if assembly.indexRemoteResults && assembly.storage.searchIndex != nil {
		// Cache swarm results into the local index (YaCy addResultsToLocalIndex),
		// but only when the full-text backend is available to make them findable.
		search = withRemoteIndexCache(
			search,
			newRemoteIndexCache(assembly.storage, assembly.storageGrowth),
		)
	}
	if assembly.swarmSeed.Enabled && assembly.seedQueue != nil {
		// Greedy learning (YaCy 1.5): crawl what swarm search surfaced, growing
		// the index from real usage — no document-count ceiling, so a large
		// index keeps discovering what it and the swarm still lack.
		search = withSwarmSeedCrawl(
			search,
			newCrawlSeeder(
				assembly.seedQueue,
				assembly.storage.documentDirectory,
				assembly.identity.Hash,
				seedProfile{
					name:     swarmSeedProfileName,
					depth:    assembly.swarmSeed.SeedDepth,
					maxPages: assembly.swarmSeed.SeedMaxPages,
					options:  assembly.autocrawlerCrawl,
				},
				assembly.maxPagesPerRun,
			),
		)
	}

	search = withInteractiveSearchBudget(search)
	search = searchsession.WithRecentSuccessOnIncompleteRefresh(search, stable)
	search = withQueryLogging(search, assembly.queryLogMode, assembly.activity)
	search = withSearchMetrics(search, assembly.searchMetrics)
	search = withEffectiveWebFallbackRequest(search, assembly.webFallback)

	return withParsedQuery(search)
}

func searchEvidenceSource(index searchindex.SearchIndex) searchindex.SearchEvidenceSource {
	source, _ := index.(searchindex.SearchEvidenceSource)

	return source
}

func assembleRankingStages(
	inner searchcore.Searcher,
	assembly publicSearchAssembly,
) searchcore.Searcher {
	evidence := assembleRankingEvidenceStages(inner, assembly)
	learned := learnedrank.NewSearcher(evidence, assembly.learnedRanker)

	return searchcore.NewFinalRankingSearcher(learned)
}

// mountPortalOpenSearch registers the portal's OpenSearch description document
// and suggestions endpoint, each gated so it is served only while the public
// portal is enabled.
func mountPortalOpenSearch(mux *http.ServeMux, toggles *runtimeToggles) {
	opensearch := publicportal.NewOpenSearch()
	mux.Handle(
		opensearch.DescribePath(),
		portalGate(toggles, http.HandlerFunc(opensearch.Describe)),
	)
	mux.Handle(
		opensearch.SuggestPath(),
		portalGate(toggles, http.HandlerFunc(opensearch.Suggest)),
	)
}

// portalGate answers 404 unless the public portal is enabled, so portal-only
// routes stay invisible while the portal is off.
func portalGate(toggles *runtimeToggles, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !toggles.PortalEnabled() {
			http.NotFound(w, r)

			return
		}
		next.ServeHTTP(w, r)
	})
}

// rootDispatcher serves the public search portal at the site root when the
// operator has enabled it, and the static landing page otherwise. The portal can
// be toggled live because the choice is made per request rather than at mount
// time.
type rootDispatcher struct {
	toggles *runtimeToggles
	portal  http.Handler
	landing http.Handler
}

func newRootDispatcher(toggles *runtimeToggles, portal http.Handler) *rootDispatcher {
	return &rootDispatcher{
		toggles: toggles,
		portal:  portal,
		landing: landing.NewEndpoint(buildVersion),
	}
}

func (d *rootDispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if d.toggles.PortalEnabled() {
		d.portal.ServeHTTP(w, r)

		return
	}
	d.landing.ServeHTTP(w, r)
}

func searchAccessPolicy(assembly publicSearchAssembly) tavilyapi.SearchAccessPolicy {
	return tavilyapi.SearchAccessPolicy{
		BearerToken: assembly.searchAPIKey,
		Authorizer:  assembly.searchAuthorizer,
	}
}

// withWebFallback wraps the searcher with the optional DDGS web-search fallback.
// The decorator is installed whenever the DDGS provider is configured and gates
// itself on the runtime enable flag, so both the Tavily API and the human search
// surfaces share one fallback path.
func withWebFallback(
	search searchcore.Searcher,
	assembly publicSearchAssembly,
) searchcore.Searcher {
	config := assembly.webFallback
	if config.Provider != webFallbackProviderDDGS || config.Privacy == webFallbackPrivacyDisabled {
		return search
	}
	provider := websearch.NewDDGSProvider(websearch.DDGSConfig{
		Client:     assembly.client,
		Backend:    config.Backend,
		MaxResults: config.MaxResults,
		SafeSearch: config.SafeSearch,
		Timeout:    config.Timeout,
		CacheTTL:   config.CacheTTL,
		Accept:     websearch.VerifiedForQuery,
	})

	opts := []websearch.Option{websearch.WithProviderBudget(
		webFallbackProviderStageBudget(config),
	)}
	if config.SeedCrawl && assembly.seedQueue != nil {
		opts = append(opts, websearch.WithSeeder(newWebCrawlSeeder(
			assembly.seedQueue,
			assembly.storage.documentDirectory,
			assembly.identity.Hash,
			webCrawlSeedProfile{
				fallback:       config,
				crawl:          assembly.autocrawlerCrawl,
				maxPagesPerRun: assembly.maxPagesPerRun,
			},
		)))
	}

	permit := webFallbackPermit(config.Privacy)
	if effectiveWebFallbackPrivacy(config) == webFallbackPrivacyAlways {
		return websearch.NewParallelSearcher(search, provider, permit, opts...)
	}

	return websearch.NewFallbackSearcher(search, provider, permit, opts...)
}

func effectiveWebFallbackPrivacy(config webFallbackConfig) webFallbackPrivacy {
	if config.Privacy == webFallbackPrivacyAlways ||
		config.Privacy == webFallbackPrivacyEnabled &&
			effectiveWebFallbackTrigger(config.Trigger) == webFallbackTriggerParallel {
		return webFallbackPrivacyAlways
	}

	return config.Privacy
}

func webFallbackPermit(privacy webFallbackPrivacy) func(searchcore.Request) bool {
	if privacy == webFallbackPrivacyEnabled || privacy == webFallbackPrivacyAlways {
		return func(searchcore.Request) bool { return true }
	}

	return func(req searchcore.Request) bool { return req.AllowWebFallback }
}
