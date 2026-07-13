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
	snapshot := store.Snapshot()
	if !snapshot.Blocks("https://sub.blocked.example/") ||
		!snapshot.Blocks("https://allowed.example/blocked") {
		t.Fatalf("snapshot = %#v", snapshot)
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
	snapshot = store.Snapshot()
	if !snapshot.IsEmpty() {
		t.Fatalf("snapshot after removal = %#v", snapshot)
	}
}

func TestSnapshotCachePublishesImmutableVersions(t *testing.T) {
	store := openStore(t)
	ctx := t.Context()
	if err := store.Add(ctx, urldenylist.KindDomain, "first.example"); err != nil {
		t.Fatal(err)
	}
	first := store.Snapshot()
	if _, err := store.Remove(ctx, urldenylist.KindDomain, "first.example"); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(ctx, urldenylist.KindDomain, "second.example"); err != nil {
		t.Fatal(err)
	}
	second := store.Snapshot()
	if !first.Blocks("https://first.example/") || first.Blocks("https://second.example/") {
		t.Fatalf("first snapshot changed after publication: %#v", first)
	}
	if second.Blocks("https://first.example/") || !second.Blocks("https://second.example/") {
		t.Fatalf("second snapshot = %#v", second)
	}
}

func TestSnapshotCacheChangesOnlyAfterDurableMutation(t *testing.T) {
	engine := newFakeEngine()
	store := fakeStore(t, engine)
	engine.failPut = true
	if err := store.Add(t.Context(), urldenylist.KindDomain, "blocked.example"); err == nil {
		t.Fatal("failed add succeeded")
	}
	snapshot := store.Snapshot()
	if !snapshot.IsEmpty() {
		t.Fatalf("snapshot after failed add = %#v", snapshot)
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
	snapshot = store.Snapshot()
	if !snapshot.Blocks("https://blocked.example/") {
		t.Fatalf("snapshot after failed remove = %#v", snapshot)
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
	snapshot := store.Snapshot()
	if !snapshot.Blocks("https://sub.blocked.example/") ||
		!snapshot.Blocks("https://exact.example/blocked") {
		t.Fatalf("snapshot = %#v", snapshot)
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
	snapshot := store.Snapshot()
	if !snapshot.Blocks("https://blocked.example/") {
		t.Fatalf("snapshot after committed add = %#v", snapshot)
	}
	if _, err := store.Remove(
		t.Context(),
		urldenylist.KindDomain,
		"blocked.example",
	); err == nil {
		t.Fatal("partially committed remove succeeded")
	}
	snapshot = store.Snapshot()
	if !snapshot.IsEmpty() {
		t.Fatalf("snapshot after committed remove = %#v", snapshot)
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
	snapshot := store.Snapshot()
	if !snapshot.Blocks("https://blocked.example/") {
		t.Fatalf("snapshot after indeterminate add = %#v", snapshot)
	}
	if _, err := store.Remove(
		t.Context(),
		urldenylist.KindDomain,
		"blocked.example",
	); err == nil {
		t.Fatal("indeterminate remove succeeded")
	}
	snapshot = store.Snapshot()
	if !snapshot.Blocks("https://blocked.example/") {
		t.Fatalf("snapshot after indeterminate remove = %#v", snapshot)
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
	snapshot := store.Snapshot()
	if !snapshot.Blocks("https://blocked.example/") {
		t.Fatalf("snapshot after indeterminate add = %#v", snapshot)
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
	snapshot = store.Snapshot()
	if !snapshot.IsEmpty() {
		t.Fatalf("snapshot after recovery = %#v", snapshot)
	}
}
