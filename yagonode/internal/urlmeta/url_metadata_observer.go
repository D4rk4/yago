package urlmeta

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const urlObserverFailed = "url metadata observer failed"

type observers []URLMetadataObserver

func (o observers) stored(
	ctx context.Context,
	tx *vault.Txn,
	hash yagomodel.Hash,
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

func (o observers) purged(ctx context.Context, tx *vault.Txn, hash yagomodel.Hash) {
	for _, observer := range o {
		if err := observer.URLPurged(tx, hash); err != nil {
			slog.WarnContext(ctx, urlObserverFailed,
				slog.String("event", "purged"),
				slog.Any("error", err),
			)
		}
	}
}
