// Package urlmeta owns the transferURL endpoint, URL intake, and URL metadata
// storage and lookup. Its published port, URLDirectory, is the only surface other
// modules import; it speaks the yacymodel vocabulary and never leaks the schema.
package urlmeta

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/nodeidentity"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
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
	Purge(ctx context.Context, tx *vault.Txn, urls []yacymodel.Hash) (PurgeResult, error)
}

type URLMetadataObserver interface {
	URLStored(tx *vault.Txn, hash yacymodel.Hash, freshness string) error
	URLPurged(tx *vault.Txn, hash yacymodel.Hash) error
}

type Receipt struct {
	Busy     bool
	Double   int
	ErrorURL []yacymodel.Hash
}

type PurgeResult struct {
	URLsDeleted int
}

func Open(
	vault *vault.Vault,
	watchers ...URLMetadataObserver,
) (URLDirectory, URLEvictor, URLReceiver, error) {
	collection, err := registerCollection(vault)
	if err != nil {
		return nil, nil, nil, err
	}

	watched := observers(watchers)
	directory := urlDirectory{vault: vault, collection: collection, observers: watched}

	return directory,
		directory,
		urlIntake{vault: vault, collection: collection, observers: watched},
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
