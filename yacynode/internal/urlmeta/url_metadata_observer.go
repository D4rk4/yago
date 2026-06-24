package urlmeta

import (
	"context"
	"log/slog"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
)

const urlObserverFailed = "url metadata observer failed"

type observers []URLMetadataObserver

func (o observers) stored(
	ctx context.Context,
	tx *boltvault.Txn,
	hash yacymodel.Hash,
	freshness string,
) {
	for _, observer := range o {
		if err := observer.URLStored(tx, hash, freshness); err != nil {
			slog.WarnContext(ctx, urlObserverFailed,
				slog.String("event", "stored"),
				slog.Any("error", err),
			)
		}
	}
}

func (o observers) purged(ctx context.Context, tx *boltvault.Txn, hash yacymodel.Hash) {
	for _, observer := range o {
		if err := observer.URLPurged(tx, hash); err != nil {
			slog.WarnContext(ctx, urlObserverFailed,
				slog.String("event", "purged"),
				slog.Any("error", err),
			)
		}
	}
}
