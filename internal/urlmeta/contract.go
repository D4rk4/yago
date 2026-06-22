// Package urlmeta owns the transferURL endpoint, URL intake, and URL metadata
// storage and lookup. Its published port, URLDirectory, is the only surface other
// modules import; it speaks the yacymodel vocabulary and never leaks the schema.
package urlmeta

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/internal/boltvault"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type URLDirectory interface {
	RowsByHash(ctx context.Context, hashes []yacymodel.Hash) ([]yacymodel.URIMetadataRow, error)
	MissingURLs(ctx context.Context, hashes []yacymodel.Hash) ([]yacymodel.Hash, error)
	Count(ctx context.Context) (int, error)
}

type URLEvictor interface {
	SelectStale(ctx context.Context, limit int) ([]yacymodel.Hash, error)
	Purge(tx *boltvault.Txn, urls []yacymodel.Hash) (PurgeResult, error)
}

type PurgeResult struct {
	URLsDeleted int
}
