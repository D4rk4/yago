package search

import (
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/rwi"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmeta"
)

type Module struct {
	Endpoint http.Handler
}

func New(
	guard httpguard.RequestGuard,
	respond httpguard.WireResponder,
	index rwi.PostingScanner,
	urls urlmeta.URLDirectory,
	postingsPerWord int,
) Module {
	// FIXME: register the search handler with a shared router here (mirroring the
	// storage-owning modules) instead of returning it in Module for cmd to mount.
	return Module{
		Endpoint: searchEndpoint{
			guard:   guard,
			respond: respond,
			searcher: searcher{
				index:           index,
				urls:            urls,
				postingsPerWord: postingsPerWord,
			},
		},
	}
}
