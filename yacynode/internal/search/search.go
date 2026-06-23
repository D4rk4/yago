package search

import (
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/rwi"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmeta"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

func MountSearch(
	router httpguard.WireRouter,
	peer httpguard.PeerIdentity,
	index rwi.PostingScanner,
	urls urlmeta.URLDirectory,
	postingsPerWord int,
) {
	endpoint := searchEndpoint{
		peer: peer,
		searcher: searcher{
			index:           index,
			urls:            urls,
			postingsPerWord: postingsPerWord,
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
