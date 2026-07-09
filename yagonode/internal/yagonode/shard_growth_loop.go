package yagonode

import (
	"context"
	"log/slog"
	"time"
)

const (
	growthPollInterval  = time.Minute
	growthSplitsPerTick = 4
	growthDoneMessage   = "storage shard pool grew"
	growthFailMessage   = "storage shard growth failed"
)

// shardGrower is the growth capability the loop drives (the vault).
type shardGrower interface {
	GrowShards(ctx context.Context, maxSplits int) (int, error)
}

// autosplitSchedule supplies the live automatic-growth switch.
type autosplitSchedule interface {
	AutosplitEnabled() bool
}

// newGrowthTicks is the poll-clock seam so tests can drive the loop.
var newGrowthTicks = func() (<-chan time.Time, func()) {
	ticker := time.NewTicker(growthPollInterval)

	return ticker.C, ticker.Stop
}

// runShardGrowthLoop grows the storage shard pool as data accumulates, checking
// each poll and splitting only while automatic growth is enabled. Splitting is
// bounded per tick so a burst of growth never stalls writes for long (ADR-0037).
func runShardGrowthLoop(ctx context.Context, store shardGrower, schedule autosplitSchedule) {
	ticks, stop := newGrowthTicks()
	defer stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			growOnce(ctx, store, schedule)
		}
	}
}

func growOnce(ctx context.Context, store shardGrower, schedule autosplitSchedule) {
	if !schedule.AutosplitEnabled() {
		return
	}
	splits, err := store.GrowShards(ctx, growthSplitsPerTick)
	if err != nil {
		slog.ErrorContext(ctx, growthFailMessage, slog.Any("error", err))

		return
	}
	if splits > 0 {
		slog.InfoContext(ctx, growthDoneMessage, slog.Int("splits", splits))
	}
}
