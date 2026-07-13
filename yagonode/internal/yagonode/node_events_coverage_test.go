package yagonode

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/eventstore"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

func TestProvisionObservabilityFailsWhenEventLogUnavailable(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	if err := v.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err := provisionObservability(context.Background(), v); err == nil {
		t.Fatal("provisionObservability should fail when the event log cannot open")
	}
}

func TestAttachDurableEventsFailsWhenStoreUnavailable(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	if err := v.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err := attachDurableEvents(
		context.Background(), v, events.NewRecorder(4),
	); err == nil {
		t.Fatal("attachDurableEvents should fail on a closed store")
	}
}

func TestEventSinkPersistToleratesAppendFailure(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	store, err := eventstore.Open(context.Background(), v)
	if err != nil {
		t.Fatalf("eventstore.Open: %v", err)
	}
	if err := v.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	persistence := newEventPersistence(store)
	persistence.Persist(events.Event{
		Time:     time.Unix(0, 0).UTC(),
		Severity: events.SeverityInfo,
		Category: events.CategoryConfig,
		Name:     "probe",
		Message:  "durable append should fail quietly",
	})
	if err := persistence.Close(t.Context()); err != nil {
		t.Fatalf("close persistence: %v", err)
	}
}
