package yagonode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/events"
)

func TestIndexRebuildSchedulerPersistsStatusAndRecordsAction(t *testing.T) {
	recorder := events.NewRecorder(4)
	scheduler := newIndexRebuildScheduler(
		filepath.Join(t.TempDir(), "search.bleve"),
		recorder,
	)
	if scheduler == nil {
		t.Fatal("disk rebuild scheduler unavailable")
	}
	pending, err := scheduler.RebuildPending(t.Context())
	if err != nil || pending {
		t.Fatalf("initial pending = %v, err = %v", pending, err)
	}
	if err := scheduler.ScheduleRebuild(t.Context()); err != nil {
		t.Fatalf("schedule rebuild: %v", err)
	}
	pending, err = scheduler.RebuildPending(t.Context())
	if err != nil || !pending {
		t.Fatalf("scheduled pending = %v, err = %v", pending, err)
	}
	recent := recorder.Recent(1)
	if len(recent) != 1 || recent[0].Name != indexRebuildScheduledEvent ||
		recent[0].Category != events.CategoryStorage {
		t.Fatalf("events = %+v", recent)
	}
}

func TestIndexRebuildSchedulerRecordsFailureAndRejectsMemoryIndex(t *testing.T) {
	if scheduler := newIndexRebuildScheduler(" ", nil); scheduler != nil {
		t.Fatal("memory index received a disk rebuild scheduler")
	}
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("file"), 0o600); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}
	recorder := events.NewRecorder(4)
	scheduler := newIndexRebuildScheduler(filepath.Join(blocked, "search.bleve"), recorder)
	if err := scheduler.ScheduleRebuild(t.Context()); err == nil {
		t.Fatal("rebuild scheduling unexpectedly succeeded")
	}
	recent := recorder.Recent(1)
	if len(recent) != 1 || recent[0].Name != indexRebuildFailedEvent ||
		recent[0].Severity != events.SeverityWarn {
		t.Fatalf("events = %+v", recent)
	}
}

func TestIndexRebuildSchedulerHonorsCancellationAndStatusFailure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	scheduler := indexRebuildScheduler{path: filepath.Join(t.TempDir(), "search.bleve")}
	if _, err := scheduler.RebuildPending(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled status = %v", err)
	}
	if err := scheduler.ScheduleRebuild(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled schedule = %v", err)
	}

	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	scheduler.path = filepath.Join(blocked, "search.bleve")
	if _, err := scheduler.RebuildPending(t.Context()); err == nil {
		t.Fatal("blocked rebuild status succeeded")
	}
}
