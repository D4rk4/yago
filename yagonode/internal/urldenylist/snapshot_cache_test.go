package urldenylist_test

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/urldenylist"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestSnapshotCacheTracksCommittedMutations(t *testing.T) {
	store := openStore(t)
	ctx := t.Context()
	if err := store.Add(ctx, urldenylist.KindDomain, "blocked.example"); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(ctx, urldenylist.KindURL, "https://allowed.example/blocked"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Snapshot(ctx)
	if err != nil || !snapshot.Blocks("https://sub.blocked.example/") ||
		!snapshot.Blocks("https://allowed.example/blocked") {
		t.Fatalf("snapshot = %#v, error = %v", snapshot, err)
	}
	if removed, err := store.Remove(
		ctx,
		urldenylist.KindDomain,
		"blocked.example",
	); err != nil ||
		!removed {
		t.Fatalf("remove domain = %t, %v", removed, err)
	}
	if removed, err := store.Remove(
		ctx,
		urldenylist.KindURL,
		"https://allowed.example/blocked",
	); err != nil ||
		!removed {
		t.Fatalf("remove url = %t, %v", removed, err)
	}
	snapshot, err = store.Snapshot(ctx)
	if err != nil || !snapshot.IsEmpty() {
		t.Fatalf("snapshot after removal = %#v, error = %v", snapshot, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.Snapshot(canceled); err == nil {
		t.Fatal("canceled snapshot succeeded")
	}
}

func TestSnapshotCacheChangesOnlyAfterDurableMutation(t *testing.T) {
	engine := newFakeEngine()
	store := fakeStore(t, engine)
	engine.failPut = true
	if err := store.Add(t.Context(), urldenylist.KindDomain, "blocked.example"); err == nil {
		t.Fatal("failed add succeeded")
	}
	snapshot, err := store.Snapshot(t.Context())
	if err != nil || !snapshot.IsEmpty() {
		t.Fatalf("snapshot after failed add = %#v, error = %v", snapshot, err)
	}
	engine.failPut = false
	if err := store.Add(t.Context(), urldenylist.KindDomain, "blocked.example"); err != nil {
		t.Fatal(err)
	}
	engine.failDel = true
	if _, err := store.Remove(
		t.Context(),
		urldenylist.KindDomain,
		"blocked.example",
	); err == nil {
		t.Fatal("failed remove succeeded")
	}
	snapshot, err = store.Snapshot(t.Context())
	if err != nil || !snapshot.Blocks("https://blocked.example/") {
		t.Fatalf("snapshot after failed remove = %#v, error = %v", snapshot, err)
	}
}

func TestSnapshotCacheLoadsPersistedEntries(t *testing.T) {
	engine := newFakeEngine()
	engine.buckets["urldenylist"] = map[string][]byte{
		"domain\x00blocked.example":            []byte(`{"addedAt":"2026-07-13T00:00:00Z"}`),
		"url\x00https://exact.example/blocked": []byte(`{"addedAt":"2026-07-13T00:00:00Z"}`),
	}
	v, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	store, err := urldenylist.Open(v, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Snapshot(t.Context())
	if err != nil || !snapshot.Blocks("https://sub.blocked.example/") ||
		!snapshot.Blocks("https://exact.example/blocked") {
		t.Fatalf("snapshot = %#v, error = %v", snapshot, err)
	}
}

func TestSnapshotCacheReconcilesDurableMutationAfterUpdateError(t *testing.T) {
	engine := newFakeEngine()
	store := fakeStore(t, engine)
	engine.updateErr = context.DeadlineExceeded
	if err := store.Add(
		t.Context(),
		urldenylist.KindDomain,
		"blocked.example",
	); err == nil {
		t.Fatal("partially committed add succeeded")
	}
	snapshot, err := store.Snapshot(t.Context())
	if err != nil || !snapshot.Blocks("https://blocked.example/") {
		t.Fatalf("snapshot after committed add = %#v, error = %v", snapshot, err)
	}
	if _, err := store.Remove(
		t.Context(),
		urldenylist.KindDomain,
		"blocked.example",
	); err == nil {
		t.Fatal("partially committed remove succeeded")
	}
	snapshot, err = store.Snapshot(t.Context())
	if err != nil || !snapshot.IsEmpty() {
		t.Fatalf("snapshot after committed remove = %#v, error = %v", snapshot, err)
	}
}

func TestSnapshotCacheFailsClosedWhenMutationStateCannotBeRead(t *testing.T) {
	engine := newFakeEngine()
	store := fakeStore(t, engine)
	engine.updateErr = context.DeadlineExceeded
	engine.failScan = true
	if err := store.Add(
		t.Context(),
		urldenylist.KindDomain,
		"blocked.example",
	); err == nil {
		t.Fatal("indeterminate add succeeded")
	}
	snapshot, err := store.Snapshot(t.Context())
	if err != nil || !snapshot.Blocks("https://blocked.example/") {
		t.Fatalf("snapshot after indeterminate add = %#v, error = %v", snapshot, err)
	}
	if _, err := store.Remove(
		t.Context(),
		urldenylist.KindDomain,
		"blocked.example",
	); err == nil {
		t.Fatal("indeterminate remove succeeded")
	}
	snapshot, err = store.Snapshot(t.Context())
	if err != nil || !snapshot.Blocks("https://blocked.example/") {
		t.Fatalf("snapshot after indeterminate remove = %#v, error = %v", snapshot, err)
	}
}

func TestSnapshotCacheClearsIndeterminateAddAfterSuccessfulRemoval(t *testing.T) {
	engine := newFakeEngine()
	store := fakeStore(t, engine)
	engine.failPut = true
	engine.failScan = true
	if err := store.Add(
		t.Context(),
		urldenylist.KindDomain,
		"blocked.example",
	); err == nil {
		t.Fatal("indeterminate add succeeded")
	}
	snapshot, err := store.Snapshot(t.Context())
	if err != nil || !snapshot.Blocks("https://blocked.example/") {
		t.Fatalf("snapshot after indeterminate add = %#v, error = %v", snapshot, err)
	}
	engine.failPut = false
	engine.failScan = false
	removed, err := store.Remove(
		t.Context(),
		urldenylist.KindDomain,
		"blocked.example",
	)
	if err != nil || removed {
		t.Fatalf("remove absent durable record = %t, %v", removed, err)
	}
	snapshot, err = store.Snapshot(t.Context())
	if err != nil || !snapshot.IsEmpty() {
		t.Fatalf("snapshot after recovery = %#v, error = %v", snapshot, err)
	}
}
