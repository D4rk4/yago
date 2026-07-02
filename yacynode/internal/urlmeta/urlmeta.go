// Package urlmeta owns the transferURL endpoint, URL intake, and URL metadata
// storage and lookup. Its published ports speak the yacymodel vocabulary and
// never leak the schema.
package urlmeta

import (
	"context"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacynode/internal/vault"
	"github.com/D4rk4/yago/yacyproto"
)

type URLDirectory interface {
	RowsByHash(ctx context.Context, hashes []yacymodel.Hash) ([]yacymodel.URIMetadataRow, error)
	MissingURLs(ctx context.Context, hashes []yacymodel.Hash) ([]yacymodel.Hash, error)
	Count(ctx context.Context) (int, error)
}

type StoredURLMetadataRows interface {
	StoredURLMetadataRows(
		ctx context.Context,
		visit func(yacymodel.URIMetadataRow) (bool, error),
	) error
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
