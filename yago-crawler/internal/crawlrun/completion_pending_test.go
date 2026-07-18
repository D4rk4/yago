package crawlrun_test

import (
	"math"
	"testing"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlrun"
)

func TestCompletionPendingReportsOutstandingCount(t *testing.T) {
	completion := crawlrun.NewCompletion()
	runID := uuid.New()

	if got := completion.Pending(runID); got != 0 {
		t.Fatalf("Pending for an unknown run = %d, want 0", got)
	}

	completion.Begin(runID, func(bool) {})
	completion.Track(runID)
	if got := completion.Pending(runID); got != 2 {
		t.Fatalf("Pending after Begin+Track = %d, want 2", got)
	}

	completion.Settle(runID)
	if got := completion.Pending(runID); got != 1 {
		t.Fatalf("Pending after one Settle = %d, want 1", got)
	}

	completion.Settle(runID)
	if got := completion.Pending(runID); got != 0 {
		t.Fatalf("Pending after the run drained = %d, want 0", got)
	}
}

func TestCompletionTrackManyRejectsInvalidGrowth(t *testing.T) {
	completion := crawlrun.NewCompletion()
	runID := uuid.New()
	completion.Begin(runID, func(bool) {})

	if !completion.TrackMany(runID, 50_000) {
		t.Fatal("TrackMany rejected a bounded pending total")
	}
	if got := completion.Pending(runID); got != 50_001 {
		t.Fatalf("pending after TrackMany = %d, want 50001", got)
	}
	if completion.TrackMany(runID, -1) {
		t.Fatal("TrackMany accepted negative growth")
	}
	if completion.TrackMany(uuid.New(), 1) {
		t.Fatal("TrackMany accepted an unknown run")
	}
	if completion.TrackMany(runID, math.MaxInt) {
		t.Fatal("TrackMany accepted overflowing growth")
	}
	if got := completion.Pending(runID); got != 50_001 {
		t.Fatalf("pending after rejected growth = %d, want 50001", got)
	}
}
