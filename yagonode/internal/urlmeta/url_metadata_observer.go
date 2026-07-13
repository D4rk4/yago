package urlmeta

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const urlObserverFailed = "url metadata observer failed"

type observers []URLMetadataObserver

type urlObserverFailure struct {
	event string
	err   error
}

func (o observers) stored(
	tx *vault.Txn,
	hash yagomodel.Hash,
	freshness string,
) []urlObserverFailure {
	failures := make([]urlObserverFailure, 0)
	for _, observer := range o {
		if err := observer.URLStored(tx, hash, freshness); err != nil {
			failures = append(failures, urlObserverFailure{event: "stored", err: err})
		}
	}

	return failures
}

func (o observers) purged(tx *vault.Txn, hash yagomodel.Hash) []urlObserverFailure {
	failures := make([]urlObserverFailure, 0)
	for _, observer := range o {
		if err := observer.URLPurged(tx, hash); err != nil {
			failures = append(failures, urlObserverFailure{event: "purged", err: err})
		}
	}

	return failures
}

func logURLObserverFailures(ctx context.Context, failures []urlObserverFailure) {
	for _, failure := range failures {
		slog.WarnContext(ctx, urlObserverFailed,
			slog.String("event", failure.event),
			slog.Any("error", failure.err),
		)
	}
}

func (r PurgeResult) ReportObserverFailures(ctx context.Context) {
	logURLObserverFailures(ctx, r.observerFailures)
}
