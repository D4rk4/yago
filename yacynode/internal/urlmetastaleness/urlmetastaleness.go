// Package urlmetastaleness orders url metadata from stalest to freshest so
// eviction can name the stalest urls without decompressing a single row. It
// observes url metadata arrivals and departures inside the caller's
// transaction, so its order never drifts from the metadata it mirrors.
package urlmetastaleness

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmeta"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
)

type StaleURLSource interface {
	StalestURLs(ctx context.Context, limit int) ([]yacymodel.Hash, error)
}

type StalenessRanking interface {
	StaleURLSource
	urlmeta.URLMetadataObserver
}

func Open(vault *vault.Vault) (StalenessRanking, error) {
	return openStalenessRanking(vault)
}
