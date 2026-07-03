package yagonode

import (
	"net/http"

	"github.com/D4rk4/yago/yacynode/internal/documentsearch"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacynode/internal/peerroster"
	"github.com/D4rk4/yago/yacynode/internal/searchcore"
	"github.com/D4rk4/yago/yacynode/internal/searchlocal"
	"github.com/D4rk4/yago/yacynode/internal/searchremote"
	"github.com/D4rk4/yago/yacynode/internal/tavilyapi"
	"github.com/D4rk4/yago/yacynode/internal/websearch"
	"github.com/D4rk4/yago/yacynode/internal/yacysearch"
)

type publicSearchAssembly struct {
	storage              nodeStorage
	roster               peerroster.Roster
	identity             nodeidentity.Identity
	dht                  dhtDistributionConfig
	client               *http.Client
	dhtSearchTargetIndex func(int) (int, error)
	searchAPIKey         string
	webFallback          webFallbackConfig
}

func mountNodePublicSearch(
	mux *http.ServeMux,
	assembly publicSearchAssembly,
) {
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
	search := withWebFallback(searchcore.NewFederatedSearcher(local, remote), assembly)
	yacysearch.Mount(mux, search)
	tavilyapi.Mount(
		mux,
		search,
		assembly.storage.documentDirectory,
		tavilyapi.SearchAccessPolicy{BearerToken: assembly.searchAPIKey},
	)
	tavilyapi.MountExtract(
		mux,
		assembly.storage.documentDirectory,
		tavilyapi.SearchAccessPolicy{BearerToken: assembly.searchAPIKey},
	)
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
	if config.Provider != webFallbackProviderDDGS {
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

	return websearch.NewFallbackSearcher(search, provider, func() bool { return config.Enabled })
}
