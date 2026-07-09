// Package urlmeta owns the transferURL endpoint, URL intake, and URL metadata
// storage and lookup. Its published ports speak the yagomodel vocabulary and
// never leak the schema.
package urlmeta

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/vault"
	"github.com/D4rk4/yago/yagoproto"
)

type URLDirectory interface {
	RowsByHash(ctx context.Context, hashes []yagomodel.Hash) ([]yagomodel.URIMetadataRow, error)
	MissingURLs(ctx context.Context, hashes []yagomodel.Hash) ([]yagomodel.Hash, error)
	Count(ctx context.Context) (int, error)
}

type StoredURLMetadataRows interface {
	StoredURLMetadataRows(
		ctx context.Context,
		visit func(yagomodel.URIMetadataRow) (bool, error),
	) error
}

type URLReceiver interface {
	Receive(ctx context.Context, rows []yagomodel.URIMetadataRow) (Receipt, error)
}

type URLEvictor interface {
	Purge(ctx context.Context, tx *vault.Txn, urls []yagomodel.Hash) (PurgeResult, error)
}

type URLMetadataObserver interface {
	URLStored(tx *vault.Txn, hash yagomodel.Hash, freshness string) error
	URLPurged(tx *vault.Txn, hash yagomodel.Hash) error
}

type Receipt struct {
	Busy     bool
	Double   int
	ErrorURL []yagomodel.Hash
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

// MountTransferURL serves the DHT-in URL-metadata endpoint. accept mirrors the
// advertised accept-remote-index capability: with it off, transfers answer
// error_not_granted the way YaCy's transferURL does when allowReceiveIndex is
// disabled.
func MountTransferURL(
	router httpguard.WireRouter,
	identity nodeidentity.Identity,
	receiver URLReceiver,
	gate *httpguard.IntakeGate,
	accept bool,
) {
	httpguard.Mount(
		router,
		yagoproto.PathTransferURL,
		yagoproto.TransferURLEndpointMethods,
		yagoproto.ParseTransferURLRequest,
		transferURLEndpoint{
			identity: identity, intake: receiver, gate: gate, accept: accept,
		}.Serve,
	)
}
