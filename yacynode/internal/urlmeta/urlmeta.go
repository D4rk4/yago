// Package urlmeta owns the transferURL endpoint, URL intake, and URL metadata
// storage and lookup. Its published port, URLDirectory, is the only surface other
// modules import; it speaks the yacymodel vocabulary and never leaks the schema.
package urlmeta

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/nodeidentity"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type URLDirectory interface {
	RowsByHash(ctx context.Context, hashes []yacymodel.Hash) ([]yacymodel.URIMetadataRow, error)
	MissingURLs(ctx context.Context, hashes []yacymodel.Hash) ([]yacymodel.Hash, error)
	Count(ctx context.Context) (int, error)
}

type URLReceiver interface {
	Receive(ctx context.Context, rows []yacymodel.URIMetadataRow) (Receipt, error)
}

type URLEvictor interface {
	SelectStale(ctx context.Context, limit int) ([]yacymodel.Hash, error)
	Purge(tx *boltvault.Txn, urls []yacymodel.Hash) (PurgeResult, error)
}

type Receipt struct {
	Busy     bool
	Double   int
	ErrorURL []yacymodel.Hash
}

type PurgeResult struct {
	URLsDeleted int
}

func Open(vault *boltvault.Vault) (URLDirectory, URLEvictor, URLReceiver, error) {
	collection, err := registerCollection(vault)
	if err != nil {
		return nil, nil, nil, err
	}

	return urlDirectory{vault: vault, collection: collection},
		urlEvictor{vault: vault, collection: collection},
		urlIntake{vault: vault, collection: collection},
		nil
}

func MountTransferURL(
	router httpguard.WireRouter,
	identity nodeidentity.Identity,
	receiver URLReceiver,
) {
	httpguard.Mount(
		router,
		yacyproto.PathTransferURL,
		yacyproto.TransferURLEndpointMethods,
		yacyproto.ParseTransferURLRequest,
		transferURLEndpoint{identity: identity, intake: receiver}.Serve,
	)
}
