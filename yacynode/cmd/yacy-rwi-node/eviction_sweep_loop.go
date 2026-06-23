package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/eviction"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/metrics"
)

const (
	evictionInterval      = time.Minute
	evictionSweptMessage  = "storage eviction swept"
	evictionFailedMessage = "storage eviction failed"
)

func runEvictionLoop(
	ctx context.Context,
	sweeper eviction.Sweeper,
	observer *metrics.EvictionMetrics,
) {
	sweepOnce(ctx, sweeper, observer)

	ticker := time.NewTicker(evictionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepOnce(ctx, sweeper, observer)
		}
	}
}

func sweepOnce(ctx context.Context, sweeper eviction.Sweeper, observer *metrics.EvictionMetrics) {
	result, err := sweeper.Sweep(ctx)
	if err != nil {
		observer.ObserveFailure()
		slog.ErrorContext(ctx, evictionFailedMessage, slog.Any("error", err))

		return
	}
	observer.Observe(result)
	if result.URLsDeleted == 0 && result.PostingsDeleted == 0 {
		return
	}
	slog.DebugContext(ctx, evictionSweptMessage,
		slog.Int("urls", result.URLsDeleted),
		slog.Int("postings", result.PostingsDeleted),
	)
}
