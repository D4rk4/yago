package eviction

import (
	"context"
	"log/slog"
	"time"
)

const (
	sweptMessage  = "storage eviction swept"
	failedMessage = "storage eviction failed"
)

type SweepObserver interface {
	Observe(Result)
	ObserveFailure()
}

func RunSweepLoop(
	ctx context.Context,
	sweeper Sweeper,
	observer SweepObserver,
	interval time.Duration,
) {
	sweepOnce(ctx, sweeper, observer)

	ticker := time.NewTicker(interval)
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

func sweepOnce(ctx context.Context, sweeper Sweeper, observer SweepObserver) {
	result, err := sweeper.Sweep(ctx)
	if err != nil {
		observer.ObserveFailure()
		slog.ErrorContext(ctx, failedMessage, slog.Any("error", err))

		return
	}
	observer.Observe(result)
	if result.URLsDeleted == 0 && result.PostingsDeleted == 0 {
		return
	}
	slog.DebugContext(ctx, sweptMessage,
		slog.Int("urls", result.URLsDeleted),
		slog.Int("postings", result.PostingsDeleted),
	)
}
