package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/eviction"
	"github.com/D4rk4/yago/yacynode/internal/metrics"
)

const (
	evictionInterval      = time.Minute
	evictionSweptMessage  = "storage eviction swept"
	evictionFailedMessage = "storage eviction failed"
)

var newEvictionTicks = func(interval time.Duration) (<-chan time.Time, func()) {
	ticker := time.NewTicker(interval)

	return ticker.C, ticker.Stop
}

func runEvictionLoop(
	ctx context.Context,
	sweeper eviction.Sweeper,
	observer *metrics.EvictionMetrics,
) {
	sweepOnce(ctx, sweeper, observer)

	ticks, stop := newEvictionTicks(evictionInterval)
	defer stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
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
