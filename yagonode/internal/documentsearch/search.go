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

func MountSearch(
	router httpguard.WireRouter,
	identity nodeidentity.Identity,
	index rwi.PostingIndex,
	documents urlmeta.URLDirectory,
	matchesPerTerm int,
) {
	endpoint := searchEndpoint{
		identity: identity,
		searcher: searcher{
			index:          index,
			documents:      documents,
			matchesPerTerm: matchesPerTerm,
		},
	}

	httpguard.Mount(
		router,
		yagoproto.PathSearch,
		yagoproto.SearchEndpointMethods,
		yagoproto.ParseSearchRequest,
		endpoint.Serve,
	)
}
