package yagonode

import (
	"context"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	compactionPollInterval = time.Minute
	compactionDoneMessage  = "storage compaction reclaimed space"
	compactionFailMessage  = "storage compaction failed"
)

// storageCompactor is the compaction capability the loop drives (the vault).
type storageCompactor interface {
	Compact(ctx context.Context) (vault.CompactResult, error)
}

// compactionSchedule supplies the live compaction cadence (0 = off).
type compactionSchedule interface {
	CompactionInterval() time.Duration
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
func runCompactionLoop(ctx context.Context, store storageCompactor, schedule compactionSchedule) {
	ticks, stop := newCompactionTicks()
	defer stop()

	var last time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticks:
			last = advanceCompaction(ctx, store, schedule.CompactionInterval(), last, now)
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
	interval time.Duration,
	last, now time.Time,
) time.Time {
	if interval <= 0 {
		return time.Time{}
	}
	if last.IsZero() {
		return now
	}
	if now.Sub(last) < interval {
		return last
	}
	compactOnce(ctx, store)

	return now
}

func compactOnce(ctx context.Context, store storageCompactor) {
	result, err := store.Compact(ctx)
	if err != nil {
		slog.ErrorContext(ctx, compactionFailMessage, slog.Any("error", err))

		return
	}
	if result.ShardsCompacted == 0 {
		return
	}
	slog.InfoContext(ctx, compactionDoneMessage,
		slog.Int("shards", result.ShardsCompacted),
		slog.Int64("bytesReclaimed", result.BytesReclaimed),
	)
}
