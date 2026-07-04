package settingsstore_test

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/settingsstore"
)

func openTestStore(t *testing.T) *settingsstore.Store {
	t.Helper()

	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })

	store, err := settingsstore.Open(v)
	if err != nil {
		t.Fatalf("settingsstore.Open: %v", err)
	}

	return store
}

func TestGetReturnsUnsetForMissingName(t *testing.T) {
	store := openTestStore(t)

	value, set, err := store.Get(context.Background(), "portal.enabled")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if set {
		t.Fatalf("set = true, want false for a missing name")
	}
	if value != "" {
		t.Fatalf("value = %q, want empty", value)
	}
}

func TestSetThenGetReturnsStoredValue(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	if err := store.Set(ctx, "portal.enabled", "true"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	value, set, err := store.Get(ctx, "portal.enabled")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !set || value != "true" {
		t.Fatalf("Get = (%q, %v), want (\"true\", true)", value, set)
	}
}

func TestSetOverwritesPreviousValue(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	if err := store.Set(ctx, "portal.enabled", "true"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store.Set(ctx, "portal.enabled", "false"); err != nil {
		t.Fatalf("Set overwrite: %v", err)
	}

	value, _, err := store.Get(ctx, "portal.enabled")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if value != "false" {
		t.Fatalf("value = %q, want false", value)
	}
}

func TestUnsetRemovesOverride(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	if err := store.Set(ctx, "portal.enabled", "true"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store.Unset(ctx, "portal.enabled"); err != nil {
		t.Fatalf("Unset: %v", err)
	}

	_, set, err := store.Get(ctx, "portal.enabled")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if set {
		t.Fatalf("set = true after Unset, want false")
	}
}

func TestUnsetMissingNameIsNoError(t *testing.T) {
	store := openTestStore(t)

	if err := store.Unset(context.Background(), "absent"); err != nil {
		t.Fatalf("Unset missing: %v", err)
	}
}

func TestAllReturnsEveryOverride(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	if err := store.Set(ctx, "portal.enabled", "true"); err != nil {
		t.Fatalf("Set portal: %v", err)
	}
	if err := store.Set(ctx, "https.redirect", "false"); err != nil {
		t.Fatalf("Set redirect: %v", err)
	}

	all, err := store.All(ctx)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 2 || all["portal.enabled"] != "true" || all["https.redirect"] != "false" {
		t.Fatalf("All = %v, want two overrides", all)
	}
}
