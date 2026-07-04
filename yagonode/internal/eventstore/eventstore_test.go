package eventstore_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/eventstore"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func testVault(t *testing.T) *vault.Vault {
	t.Helper()

	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })

	return storage
}

func event(name string) events.Event {
	return events.Event{
		Time:     time.Unix(0, 0).UTC(),
		Severity: events.SeverityInfo,
		Category: events.CategoryConfig,
		Name:     name,
		Message:  "message " + name,
	}
}

func names(evs []events.Event) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.Name
	}

	return out
}

func TestStoreAppendAndRecent(t *testing.T) {
	ctx := context.Background()
	store, err := eventstore.Open(ctx, testVault(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	for _, name := range []string{"a", "b", "c"} {
		if err := store.Append(ctx, event(name)); err != nil {
			t.Fatalf("append %q: %v", name, err)
		}
	}
	recent, err := store.Recent(ctx)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if got := names(recent); len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("recent = %v", got)
	}
	if recent[0].Category != events.CategoryConfig || recent[0].Message != "message a" {
		t.Fatalf("event not round-tripped: %+v", recent[0])
	}
}

func TestStorePrunesBeyondCapacity(t *testing.T) {
	ctx := context.Background()
	store, err := eventstore.OpenWithCapacity(ctx, testVault(t), 3)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	for _, name := range []string{"1", "2", "3", "4", "5"} {
		if err := store.Append(ctx, event(name)); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	recent, err := store.Recent(ctx)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if got := names(recent); len(got) != 3 || got[0] != "3" || got[2] != "5" {
		t.Fatalf("recent = %v, want the newest three", got)
	}
}

func TestStoreResumesSequenceAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "node.db")

	first, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	store, err := eventstore.OpenWithCapacity(ctx, first, 10)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.Append(ctx, event("a")); err != nil {
		t.Fatalf("append a: %v", err)
	}
	if err := store.Append(ctx, event("b")); err != nil {
		t.Fatalf("append b: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}

	reopened, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen vault: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	resumed, err := eventstore.OpenWithCapacity(ctx, reopened, 10)
	if err != nil {
		t.Fatalf("resume store: %v", err)
	}
	if err := resumed.Append(ctx, event("c")); err != nil {
		t.Fatalf("append c: %v", err)
	}

	recent, err := resumed.Recent(ctx)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if got := names(recent); len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("recent = %v; the resumed store must not overwrite prior events", got)
	}
}
