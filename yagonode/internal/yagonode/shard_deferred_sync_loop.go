package yagonode

import (
	"context"
	"log/slog"
	"time"
)

const (
	deferredSyncPollInterval = 30 * time.Second
	deferredSyncFailMessage  = "storage deferred-fsync flush failed"
)

// deferredSyncer is the flush capability the loop drives (the vault): whether
// the engine is deferring fsync and the pass that flushes the shards.
type deferredSyncer interface {
	DeferredFsyncEnabled() bool
	SyncShards(ctx context.Context) error
}

// newDeferredSyncTicks is the poll-clock seam so tests can drive the loop.
var newDeferredSyncTicks = func() (<-chan time.Time, func()) {
	ticker := time.NewTicker(deferredSyncPollInterval)

	return ticker.C, ticker.Stop
}

// runDeferredSyncLoop periodically flushes the storage engine's deferred writes
// to disk while it runs in deferred-fsync mode (ADR-0038). With per-commit fsync
// (the default) the engine reports the mode off and every tick is a no-op, so
// the loop runs unconditionally and costs nothing when the mode is disabled.
func runDeferredSyncLoop(ctx context.Context, store deferredSyncer) {
	ticks, stop := newDeferredSyncTicks()
	defer stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			syncOnce(ctx, store)
		}
	}
}

func syncOnce(ctx context.Context, store deferredSyncer) {
	if !store.DeferredFsyncEnabled() {
		return
	}
	if err := store.SyncShards(ctx); err != nil {
		slog.ErrorContext(ctx, deferredSyncFailMessage, slog.Any("error", err))
	}
}
