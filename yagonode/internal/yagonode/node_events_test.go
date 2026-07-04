package yagonode

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/events"
)

func TestAttachDurableEventsPersistsAcrossRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "node.db")

	first, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	recorder := events.NewRecorder(events.DefaultCapacity)
	if err := attachDurableEvents(ctx, first, recorder); err != nil {
		t.Fatalf("attach: %v", err)
	}
	recorder.Record(events.SeverityInfo, events.CategoryConfig, "first", "message")
	recorder.Record(events.SeverityWarn, events.CategoryDHT, "second", "message")
	if err := first.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}

	reopened, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen vault: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	restored := events.NewRecorder(events.DefaultCapacity)
	if err := attachDurableEvents(ctx, reopened, restored); err != nil {
		t.Fatalf("reattach: %v", err)
	}

	recent := restored.Recent(0)
	if len(recent) != 2 || recent[0].Name != "second" || recent[1].Name != "first" {
		t.Fatalf("restored recorder did not resume the durable log: %+v", recent)
	}
}
