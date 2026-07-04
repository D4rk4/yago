package yagonode

import (
	"net/http"

	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
	"github.com/D4rk4/yago/yagonode/internal/documentsearch"
	"github.com/D4rk4/yago/yagonode/internal/landing"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
	"github.com/D4rk4/yago/yagonode/internal/publicportal"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
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
	dhtSearchTargetIndex func(int) (int, error)
	searchAPIKey         string
	searchAuthorizer     tavilyapi.ScopeAuthorizer
	extractFetcher       tavilyapi.ContentFetcher
	webFallback          webFallbackConfig
	seedQueue            crawldispatch.CrawlOrderQueue
	toggles              *runtimeToggles
	queryLogMode         queryLogMode
	searchMetrics        *metrics.SearchMetrics
}

func mountNodePublicSearch(
	mux *http.ServeMux,
	assembly publicSearchAssembly,
) searchcore.Searcher {
	local := searchlocal.NewSearcher(assembly.storage.searchIndex)
	if assembly.storage.searchIndex == nil {
		local = documentsearch.NewLocalSearcherWithDocuments(
			assembly.storage.postings,
			assembly.storage.urlDirectory,
			assembly.storage.documentDirectory,
			searchPostingsPerWord,
		)
	}
	remote := searchremote.NewSearcher(searchremote.Config{
		Client:             assembly.client,
		NetworkName:        assembly.identity.NetworkName,
		Peers:              assembly.roster,
		Redundancy:         assembly.dht.Redundancy,
		MinimumPeerAgeDays: assembly.dht.MinimumPeerAgeDays,
		PartitionExponent:  assembly.dht.PartitionExponent,
		RandomTargetIndex:  assembly.dhtSearchTargetIndex,
	})
	federated := withWebFallback(searchcore.NewFederatedSearcher(local, remote), assembly)
	search := withQueryLogging(federated, assembly.queryLogMode)
	search = withSearchMetrics(search, assembly.searchMetrics)
	access := searchAccessPolicy(assembly)
	yacysearch.Mount(mux, search)
	tavilyapi.Mount(mux, search, assembly.storage.documentDirectory, access)
	tavilyapi.MountExtract(mux, assembly.storage.documentDirectory, access, assembly.extractFetcher)
	mux.Handle(
		"/{$}",
		newRootDispatcher(assembly.toggles, publicportal.New(newPortalSource(search))),
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
	return &rootDispatcher{toggles: toggles, portal: portal, landing: landing.NewEndpoint()}
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
