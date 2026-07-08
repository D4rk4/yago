// Package documentsearch finds documents containing wanted terms, orders them by
// relevance, and reports how many documents matched each term.
package documentsearch

import (
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/nodeidentity"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/rwi"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmeta"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
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
		yacyproto.PathSearch,
		yacyproto.SearchEndpointMethods,
		yacyproto.ParseSearchRequest,
		endpoint.Serve,
	)
}
