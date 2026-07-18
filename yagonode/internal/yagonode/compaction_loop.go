package yagonode

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	compactionPollInterval      = time.Minute
	compactionDoneMessage       = "storage compaction reclaimed space"
	compactionFailMessage       = "storage compaction failed"
	compactionDeferredMessage   = "storage compaction deferred"
	compactionHeadroomReason    = "insufficient temporary-copy headroom"
	compactionMeasurementReason = "temporary-copy headroom unavailable"
)

// storageCompactor is the compaction capability the loop drives (the vault).
type storageCompactor interface {
	Compact(ctx context.Context) (vault.CompactResult, error)
	CompactionHeadroom(ctx context.Context) (uint64, error)
}

// compactionSchedule supplies the live compaction cadence (0 = off).
type compactionSchedule interface {
	CompactionInterval() time.Duration
}

type compactionWindow struct {
	interval time.Duration
	last     time.Time
	now      time.Time
}

// newCompactionTicks is the poll-clock seam so tests can drive the loop.
var newCompactionTicks = func() (<-chan time.Time, func()) {
	ticker := time.NewTicker(compactionPollInterval)

	return ticker.C, ticker.Stop
}

// runCompactionLoop compacts the store every configured interval, re-reading the
// live cadence each poll so an operator's change takes effect without a restart.
// An interval of 0 disables it. The first compaction lands one full interval
// after the loop starts observing, never at startup, and disabling resets that
// baseline so re-enabling waits a fresh interval.
func runCompactionLoop(
	ctx context.Context,
	store storageCompactor,
	schedule compactionSchedule,
	admissions ...growthAdmission,
) {
	ticks, stop := newCompactionTicks()
	defer stop()

	var last time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticks:
			last = advanceCompaction(
				ctx,
				store,
				compactionWindow{
					interval: schedule.CompactionInterval(),
					last:     last,
					now:      now,
				},
				admissions...,
			)
		}
	}
}

// advanceCompaction decides whether a compaction is due at now and returns the
// updated baseline. It compacts (and resets the baseline to now) once a full
// interval has elapsed since the last baseline; a zero interval disables the
// pass and clears the baseline.
func advanceCompaction(
	ctx context.Context,
	store storageCompactor,
	window compactionWindow,
	admissions ...growthAdmission,
) time.Time {
	if window.interval <= 0 {
		return time.Time{}
	}
	if window.last.IsZero() {
		return window.now
	}
	if window.now.Sub(window.last) < window.interval {
		return window.last
	}
	if !compactOnce(ctx, store, admissions...) {
		return window.last
	}

	return window.now
}

func compactOnce(
	ctx context.Context,
	store storageCompactor,
	admissions ...growthAdmission,
) bool {
	var admission growthAdmission
	if len(admissions) > 0 {
		admission = admissions[0]
	}
	result := vault.CompactResult{}
	outcome, err := runStorageMaintenance(
		admission,
		func() (uint64, error) {
			return store.CompactionHeadroom(ctx)
		},
		func(required uint64) error {
			if required == 0 {
				return nil
			}
			var compactErr error
			result, compactErr = store.Compact(ctx)
			if compactErr != nil {
				return fmt.Errorf("compact storage: %w", compactErr)
			}

			return nil
		},
	)
	if err != nil && !outcome.Measured {
		slog.WarnContext(
			ctx,
			compactionDeferredMessage,
			slog.String("reason", compactionMeasurementReason),
			slog.Any("error", err),
		)

		return false
	}
	if err != nil && !outcome.Started {
		slog.WarnContext(
			ctx,
			compactionDeferredMessage,
			slog.String("reason", compactionHeadroomReason),
			slog.Uint64("requiredBytes", outcome.RequiredBytes),
		)

		return false
	}
	if err != nil {
		slog.ErrorContext(ctx, compactionFailMessage, slog.Any("error", err))

		return true
	}
	if result.ShardsCompacted == 0 {
		return true
	}
	slog.InfoContext(ctx, compactionDoneMessage,
		slog.Int("shards", result.ShardsCompacted),
		slog.Int64("bytesReclaimed", result.BytesReclaimed),
	)

	return true
}
