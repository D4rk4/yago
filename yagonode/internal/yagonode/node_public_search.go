package yagonode

import (
	"context"
	"net/http"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
	"github.com/D4rk4/yago/yagonode/internal/documentsearch"
	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/landing"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
	"github.com/D4rk4/yago/yagonode/internal/publicportal"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/searchlocal"
	"github.com/D4rk4/yago/yagonode/internal/searchremote"
	"github.com/D4rk4/yago/yagonode/internal/tavilyapi"
	"github.com/D4rk4/yago/yagonode/internal/websearch"
	"github.com/D4rk4/yago/yagonode/internal/yacysearch"
)

type publicSearchAssembly struct {
	storage              nodeStorage
	roster               peerroster.Roster
	identity             nodeidentity.Identity
	dht                  dhtDistributionConfig
	client               *http.Client
	peerClient           *http.Client
	peerHTTPSPreferred   bool
	dhtSearchTargetIndex func(int) (int, error)
	searchAPIKey         string
	searchAuthorizer     tavilyapi.ScopeAuthorizer
	extractFetcher       tavilyapi.ContentFetcher
	webFallback          webFallbackConfig
	seedQueue            crawldispatch.CrawlOrderQueue
	toggles              *runtimeToggles
	queryLogMode         queryLogMode
	searchMetrics        *metrics.SearchMetrics
	rankingWeights       func() searchindex.RankingWeights
	hostRank             func() hostrank.Table
	denylist             denylistSnapshotter
	indexRemoteResults   bool
	swarmSeed            swarmSeedConfig
	linksNewTab          bool
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
	if len(req.Terms) == 0 && strings.TrimSpace(req.Query) != "" {
		parsed := searchcore.ParseTextQuery(req.Query)
		req.Terms = parsed.Terms
		req.ExcludedTerms = parsed.ExcludedTerms
		req.Phrases = parsed.Phrases()
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

func mountNodePublicSearch(
	mux *http.ServeMux,
	assembly publicSearchAssembly,
) searchcore.Searcher {
	local := searchlocal.NewSearcherWithRanking(
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
	remote := searchremote.NewSearcher(searchremote.Config{
		Client:             remoteSearchClient(assembly),
		NetworkName:        assembly.identity.NetworkName,
		Peers:              searchTargetPeers{roster: assembly.roster},
		Redundancy:         assembly.dht.Redundancy,
		MinimumPeerAgeDays: assembly.dht.MinimumPeerAgeDays,
		PartitionExponent:  assembly.dht.PartitionExponent,
		RandomTargetIndex:  assembly.dhtSearchTargetIndex,
		Weights:            remoteRankingWeights(assembly.rankingWeights),
		PreferHTTPS:        assembly.peerHTTPSPreferred,
	})
	federated := withWebFallback(searchcore.NewFederatedSearcher(local, remote), assembly)
	filtered := withDenylistFilter(federated, assembly.denylist)
	search := withQueryLogging(filtered, assembly.queryLogMode)
	search = withSearchMetrics(search, assembly.searchMetrics)
	search = withParsedQuery(search)
	if assembly.indexRemoteResults && assembly.storage.searchIndex != nil {
		// Cache swarm results into the local index (YaCy addResultsToLocalIndex),
		// but only when the full-text backend is available to make them findable.
		search = withRemoteIndexCache(search, newRemoteIndexCache(assembly.storage))
	}
	if assembly.swarmSeed.Enabled && assembly.seedQueue != nil {
		// Greedy learning (YaCy 1.5): crawl what swarm search surfaced, growing
		// the index from real usage until the document limit is reached.
		search = withSwarmSeedCrawl(
			search,
			newCrawlSeeder(
				assembly.seedQueue,
				assembly.storage.documentDirectory,
				assembly.identity.Hash,
				seedProfile{name: swarmSeedProfileName, depth: 1, maxPages: 20},
			),
			assembly.storage.documentDirectory,
			assembly.swarmSeed.LimitDocs,
		)
	}
	access := searchAccessPolicy(assembly)
	// Autocomplete suggestions come from the local index alone (denylist applied,
	// same as served results) so typeahead never fans out to the swarm or the web
	// fallback that the main search path can reach.
	suggestSource := withDenylistFilter(local, assembly.denylist)
	yacysearch.Mount(mux, search, suggestSource, assembly.linksNewTab)
	tavilyapi.Mount(mux, search, assembly.storage.documentDirectory, access)
	tavilyapi.MountExtract(mux, assembly.storage.documentDirectory, access, assembly.extractFetcher)
	mux.Handle(
		"/{$}",
		newRootDispatcher(
			assembly.toggles,
			publicportal.New(newPortalSource(search), assembly.linksNewTab),
		),
	)
	mountPortalOpenSearch(mux, assembly.toggles)

	return search
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

// searchAccessPolicy prefers scoped API-key auth when the operator requires it,
// falling back to the legacy static bearer token (or public access when neither
// is configured).
func searchAccessPolicy(assembly publicSearchAssembly) tavilyapi.SearchAccessPolicy {
	if assembly.searchAuthorizer != nil {
		return tavilyapi.SearchAccessPolicy{Authorizer: assembly.searchAuthorizer}
	}

	return tavilyapi.SearchAccessPolicy{BearerToken: assembly.searchAPIKey}
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
	})

	var opts []websearch.Option
	if config.SeedCrawl && assembly.seedQueue != nil {
		opts = append(opts, websearch.WithSeeder(newWebCrawlSeeder(
			assembly.seedQueue,
			assembly.storage.documentDirectory,
			assembly.identity.Hash,
			config,
		)))
	}

	return websearch.NewFallbackSearcher(
		search,
		provider,
		webFallbackPermit(config.Privacy),
		opts...,
	)
}

// webFallbackPermit maps the privacy mode to the per-request decision the
// fallback searcher applies: enabled permits every query, while explicit permits
// only a query that opted in. Disabled is handled before installation.
func webFallbackPermit(privacy webFallbackPrivacy) func(searchcore.Request) bool {
	if privacy == webFallbackPrivacyEnabled {
		return func(searchcore.Request) bool { return true }
	}

	return func(req searchcore.Request) bool { return req.AllowWebFallback }
}
