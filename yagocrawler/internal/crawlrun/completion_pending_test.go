package crawlrun_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yagocrawler/internal/crawlrun"
)

func TestCompletionPendingReportsOutstandingCount(t *testing.T) {
	completion := crawlrun.NewCompletion()
	runID := uuid.New()

	if got := completion.Pending(runID); got != 0 {
		t.Fatalf("Pending for an unknown run = %d, want 0", got)
	}

	completion.Begin(runID, func() {})
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
