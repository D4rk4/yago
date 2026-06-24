package urlmeta

import (
	"context"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
)

type urlEvictor struct {
	vault      *boltvault.Vault
	collection *boltvault.Collection[yacymodel.URIMetadataRow]
	observers  observers
}

func (e urlEvictor) Purge(
	ctx context.Context,
	tx *boltvault.Txn,
	urls []yacymodel.Hash,
) (PurgeResult, error) {
	var result PurgeResult
	for _, hash := range urls {
		deleted, err := e.collection.Delete(tx, boltvault.Key(hash))
		if err != nil {
			return PurgeResult{}, fmt.Errorf("delete url metadata: %w", err)
		}
		if !deleted {
			continue
		}
		e.observers.purged(ctx, tx, hash)
		result.URLsDeleted++
	}

	return result, nil
}

var _ URLEvictor = urlEvictor{}
