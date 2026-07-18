package yagonode

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const (
	growthPollInterval    = time.Minute
	growthSplitsPerTick   = 4
	growthDoneMessage     = "storage shard pool grew"
	growthFailMessage     = "storage shard growth failed"
	growthDeferredMessage = "storage shard growth deferred"
)

// shardGrower is the growth capability the loop drives (the vault).
type shardGrower interface {
	GrowShards(ctx context.Context, maxSplits int) (int, error)
}

type shardGrowthHeadroomSource interface {
	ShardGrowthHeadroom(ctx context.Context) (uint64, error)
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
func runShardGrowthLoop(
	ctx context.Context,
	store shardGrower,
	schedule autosplitSchedule,
	admissions ...growthAdmission,
) {
	ticks, stop := newGrowthTicks()
	defer stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			growOnce(ctx, store, schedule, admissions...)
		}
	}
}

func growOnce(
	ctx context.Context,
	store shardGrower,
	schedule autosplitSchedule,
	admissions ...growthAdmission,
) {
	if !schedule.AutosplitEnabled() {
		return
	}
	var admission growthAdmission
	if len(admissions) > 0 {
		admission = admissions[0]
	}
	splits := 0
	for splits < growthSplitsPerTick {
		grew, shouldGrow, outcome, err := attemptShardGrowth(ctx, store, admission)
		if err != nil && !outcome.Measured {
			slog.WarnContext(
				ctx,
				growthDeferredMessage,
				slog.String("reason", "temporary-copy headroom unavailable"),
				slog.Any("error", err),
			)

			return
		}
		if err != nil && !outcome.Started {
			slog.WarnContext(
				ctx,
				growthDeferredMessage,
				slog.String("reason", "insufficient temporary-copy headroom"),
				slog.Uint64("requiredBytes", outcome.RequiredBytes),
			)
			break
		}
		if err != nil {
			slog.ErrorContext(ctx, growthFailMessage, slog.Any("error", err))

			return
		}
		if !shouldGrow || grew == 0 {
			break
		}
		splits += grew
	}
	if splits > 0 {
		slog.InfoContext(ctx, growthDoneMessage, slog.Int("splits", splits))
	}
}

func attemptShardGrowth(
	ctx context.Context,
	store shardGrower,
	admission growthAdmission,
) (int, bool, storageMaintenanceOutcome, error) {
	headroom, measuresHeadroom := store.(shardGrowthHeadroomSource)
	shouldGrow := !measuresHeadroom
	grew := 0
	outcome, err := runStorageMaintenance(
		admission,
		func() (uint64, error) {
			if !measuresHeadroom {
				return 0, nil
			}
			required, measureErr := headroom.ShardGrowthHeadroom(ctx)
			if measureErr != nil {
				return 0, fmt.Errorf("measure shard growth headroom: %w", measureErr)
			}
			shouldGrow = required > 0

			return required, nil
		},
		func(uint64) error {
			if !shouldGrow {
				return nil
			}
			var growErr error
			grew, growErr = store.GrowShards(ctx, 1)
			if growErr != nil {
				return fmt.Errorf("grow storage shards: %w", growErr)
			}

			return nil
		},
	)

	return grew, shouldGrow, outcome, err
}
