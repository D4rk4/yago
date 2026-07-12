package adminauth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newTestKeyStore(t *testing.T) (*apiKeyStore, *scriptedEngine, *mutableClock) {
	t.Helper()
	engine := newScriptedEngine()
	clock := &mutableClock{now: time.Unix(1000, 0)}
	store, err := newAPIKeyStore(scriptedVault(t, engine), clock.Now)
	if err != nil {
		t.Fatalf("newAPIKeyStore: %v", err)
	}

	return store, engine, clock
}

func TestAPIKeyStoreCreateProducesUsableKey(t *testing.T) {
	store, _, clock := newTestKeyStore(t)
	created, err := store.create(context.Background(), "ci", []Scope{ScopeAdminRead})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id, _, ok := parseAPIKey(created.Key); !ok || id != created.ID {
		t.Fatalf("created key %q does not parse to id %q", created.Key, created.ID)
	}
	if !created.CreatedAt.Equal(clock.now) {
		t.Fatalf("CreatedAt = %v, want %v", created.CreatedAt, clock.now)
	}
}

func TestAPIKeyStoreCreateSurfacesPutError(t *testing.T) {
	store, engine, _ := newTestKeyStore(t)
	engine.putErr = errors.New("disk full")
	if _, err := store.create(context.Background(), "ci", []Scope{ScopeAdminRead}); err == nil {
		t.Fatal("create should surface the store write error")
	}
}

func TestAPIKeyStoreCreateSurfacesIDRandomError(t *testing.T) {
	store, _, _ := newTestKeyStore(t)
	original := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }
	t.Cleanup(func() { randRead = original })
	if _, err := store.create(context.Background(), "ci", []Scope{ScopeAdminRead}); err == nil {
		t.Fatal("create should fail when the identifier random source fails")
	}
}

func TestAPIKeyStoreCreateSurfacesSecretRandomError(t *testing.T) {
	store, _, _ := newTestKeyStore(t)
	original := randRead
	calls := 0
	randRead = func(buf []byte) (int, error) {
		calls++
		if calls == 1 {
			return original(buf)
		}

		return 0, errors.New("no entropy")
	}
	t.Cleanup(func() { randRead = original })
	if _, err := store.create(context.Background(), "ci", []Scope{ScopeAdminRead}); err == nil {
		t.Fatal("create should fail when the secret random source fails")
	}
}

func TestAPIKeyStoreAuthenticateSucceedsWithoutTouchingLastUsed(t *testing.T) {
	store, _, clock := newTestKeyStore(t)
	created, err := store.create(
		context.Background(),
		"ci",
		[]Scope{ScopeAdminRead, ScopeCrawlWrite},
	)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	clock.now = time.Unix(2000, 0)
	info, ok, err := store.authenticate(context.Background(), created.Key)
	if err != nil || !ok {
		t.Fatalf("authenticate = %v, %v", ok, err)
	}
	if info.ID != created.ID || !info.hasScope(ScopeCrawlWrite) {
		t.Fatalf("info = %#v", info)
	}
	if !info.LastUsedAt.IsZero() {
		t.Fatalf("LastUsedAt = %v, want zero", info.LastUsedAt)
	}
	if err := store.touchLastUsed(context.Background(), created.ID); err != nil {
		t.Fatalf("touchLastUsed: %v", err)
	}
	infos, err := store.list(context.Background())
	if err != nil || len(infos) != 1 || !infos[0].LastUsedAt.Equal(clock.now) {
		t.Fatalf("list after touch = %#v, %v", infos, err)
	}
}

func TestAPIKeyStoreAuthenticateRejectsMalformedKey(t *testing.T) {
	store, _, _ := newTestKeyStore(t)
	_, ok, err := store.authenticate(context.Background(), "not-a-key")
	if err != nil || ok {
		t.Fatalf("authenticate = %v, %v", ok, err)
	}
}

func TestAPIKeyStoreAuthenticateRejectsUnknownID(t *testing.T) {
	store, engine, _ := newTestKeyStore(t)
	created, err := store.create(context.Background(), "ci", []Scope{ScopeAdminRead})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	delete(engine.buckets[adminAPIKeysBucket], created.ID)
	_, ok, err := store.authenticate(context.Background(), created.Key)
	if err != nil || ok {
		t.Fatalf("authenticate = %v, %v", ok, err)
	}
}

func TestAPIKeyStoreAuthenticateRejectsWrongSecret(t *testing.T) {
	store, _, _ := newTestKeyStore(t)
	created, err := store.create(context.Background(), "ci", []Scope{ScopeAdminRead})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id, secret, _ := parseAPIKey(created.Key)
	flip := byte('A')
	if secret[0] == 'A' {
		flip = 'B'
	}
	forged := formatAPIKey(id, string(flip)+secret[1:])
	_, ok, err := store.authenticate(context.Background(), forged)
	if err != nil || ok {
		t.Fatalf("authenticate = %v, %v", ok, err)
	}
}

func TestAPIKeyStoreAuthenticateSurfacesDecodeError(t *testing.T) {
	store, engine, _ := newTestKeyStore(t)
	created, err := store.create(context.Background(), "ci", []Scope{ScopeAdminRead})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	engine.buckets[adminAPIKeysBucket][created.ID] = []byte("{corrupt")
	if _, _, err := store.authenticate(context.Background(), created.Key); err == nil {
		t.Fatal("authenticate should surface the decode error")
	}
}

func TestAPIKeyStoreTouchLastUsedSurfacesPutError(t *testing.T) {
	store, engine, _ := newTestKeyStore(t)
	created, err := store.create(context.Background(), "ci", []Scope{ScopeAdminRead})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	engine.putErr = errors.New("disk full")
	if err := store.touchLastUsed(context.Background(), created.ID); err == nil {
		t.Fatal("touchLastUsed should surface the write error")
	}
}

func TestAPIKeyStoreTouchLastUsedIgnoresRevokedKey(t *testing.T) {
	store, _, _ := newTestKeyStore(t)
	if err := store.touchLastUsed(context.Background(), "missing"); err != nil {
		t.Fatalf("touchLastUsed: %v", err)
	}
}

func TestAPIKeyStoreTouchLastUsedSurfacesDecodeError(t *testing.T) {
	store, engine, _ := newTestKeyStore(t)
	created, err := store.create(context.Background(), "ci", []Scope{ScopeAdminRead})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	engine.buckets[adminAPIKeysBucket][created.ID] = []byte("{corrupt")
	if err := store.touchLastUsed(context.Background(), created.ID); err == nil {
		t.Fatal("touchLastUsed should surface the decode error")
	}
}

func TestAPIKeyStoreListReturnsEmpty(t *testing.T) {
	store, _, _ := newTestKeyStore(t)
	keys, err := store.list(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("list = %v, want empty", keys)
	}
}

func TestAPIKeyStoreListSortsByCreation(t *testing.T) {
	store, _, clock := newTestKeyStore(t)
	clock.now = time.Unix(3000, 0)
	second, _ := store.create(context.Background(), "second", []Scope{ScopeAdminRead})
	clock.now = time.Unix(1000, 0)
	first, _ := store.create(context.Background(), "first", []Scope{ScopeAdminRead})

	keys, err := store.list(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 2 || keys[0].ID != first.ID || keys[1].ID != second.ID {
		t.Fatalf("list order = %#v", keys)
	}
}

func TestNewSurfacesAPIKeyStoreError(t *testing.T) {
	storage := testVault(t)
	if _, err := newAPIKeyStore(storage, time.Now); err != nil {
		t.Fatalf("pre-register api keys: %v", err)
	}
	if _, err := New(storage, Config{}); err == nil {
		t.Fatal("New should fail when the api key bucket is already registered")
	}
}

func TestAPIKeyStoreListTieBreaksByID(t *testing.T) {
	store, _, _ := newTestKeyStore(t)
	first, _ := store.create(context.Background(), "a", []Scope{ScopeAdminRead})
	second, _ := store.create(context.Background(), "b", []Scope{ScopeAdminRead})
	lo, hi := first.ID, second.ID
	if lo > hi {
		lo, hi = hi, lo
	}
	keys, err := store.list(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 2 || keys[0].ID != lo || keys[1].ID != hi {
		t.Fatalf("tie-break order = %#v", keys)
	}
}

func TestAPIKeyStoreListSurfacesDecodeError(t *testing.T) {
	store, engine, _ := newTestKeyStore(t)
	created, err := store.create(context.Background(), "ci", []Scope{ScopeAdminRead})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	engine.buckets[adminAPIKeysBucket][created.ID] = []byte("{corrupt")
	if _, err := store.list(context.Background()); err == nil {
		t.Fatal("list should surface the decode error")
	}
}

func TestAPIKeyStoreDeleteRemovesKey(t *testing.T) {
	store, _, _ := newTestKeyStore(t)
	created, err := store.create(context.Background(), "ci", []Scope{ScopeAdminRead})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	deleted, err := store.delete(context.Background(), created.ID)
	if err != nil || !deleted {
		t.Fatalf("delete = %v, %v", deleted, err)
	}
	if _, ok, _ := store.authenticate(context.Background(), created.Key); ok {
		t.Fatal("authenticate should fail after delete")
	}
}

func TestAPIKeyStoreDeleteReportsMissing(t *testing.T) {
	store, _, _ := newTestKeyStore(t)
	deleted, err := store.delete(context.Background(), "missing")
	if err != nil || deleted {
		t.Fatalf("delete = %v, %v", deleted, err)
	}
}

func TestAPIKeyStoreDeleteSurfacesError(t *testing.T) {
	store, engine, _ := newTestKeyStore(t)
	created, err := store.create(context.Background(), "ci", []Scope{ScopeAdminRead})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	engine.deleteErr = errors.New("locked")
	if _, err := store.delete(context.Background(), created.ID); err == nil {
		t.Fatal("delete should surface the store error")
	}
}
