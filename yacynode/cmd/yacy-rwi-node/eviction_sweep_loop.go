package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/eviction"
)

const (
	evictionInterval      = time.Minute
	evictionSweptMessage  = "storage eviction swept"
	evictionFailedMessage = "storage eviction failed"
)

func runEvictionLoop(ctx context.Context, sweeper eviction.Sweeper) {
	sweepOnce(ctx, sweeper)

	ticker := time.NewTicker(evictionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepOnce(ctx, sweeper)
		}
	}
}

func sweepOnce(ctx context.Context, sweeper eviction.Sweeper) {
	result, err := sweeper.Sweep(ctx)
	if err != nil {
		slog.ErrorContext(ctx, evictionFailedMessage, slog.Any("error", err))

		return
	}
	if result.URLsDeleted == 0 && result.PostingsDeleted == 0 {
		return
	}
	slog.DebugContext(ctx, evictionSweptMessage,
		slog.Int("urls", result.URLsDeleted),
		slog.Int("postings", result.PostingsDeleted),
	)
}
