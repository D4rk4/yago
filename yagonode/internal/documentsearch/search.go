// Package documentsearch finds documents containing wanted terms, orders them by
// relevance, and reports how many documents matched each term.
package documentsearch

import (
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
	"github.com/D4rk4/yago/yagoproto"
)

// SearchConfig carries the backing stores and limits for the inbound YaCy
// remote-search endpoint.
type SearchConfig struct {
	Index          rwi.PostingIndex
	Documents      urlmeta.URLDirectory
	MatchesPerTerm int
	Gate           *httpguard.IntakeGate
}

func MountSearch(
	router httpguard.WireRouter,
	identity nodeidentity.Identity,
	config SearchConfig,
) {
	endpoint := searchEndpoint{
		identity: identity,
		searcher: searcher{
			index:          config.Index,
			documents:      config.Documents,
			matchesPerTerm: config.MatchesPerTerm,
		},
		gate: config.Gate,
	}

	httpguard.Mount(
		router,
		yagoproto.PathSearch,
		yagoproto.SearchEndpointMethods,
		yagoproto.ParseSearchRequest,
		endpoint.Serve,
	)
}
