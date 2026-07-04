package adminauth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSessionStoreCreateAndLookup(t *testing.T) {
	ctx := context.Background()
	base := time.Unix(1_000_000, 0)
	store, err := newSessionStore(testVault(t), time.Hour, fixedNow(base))
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}

	sess, err := store.create(ctx, "admin")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sess.Token == "" || sess.CSRFToken == "" {
		t.Fatal("token and csrf must be populated")
	}
	if !sess.ExpiresAt.Equal(base.Add(time.Hour)) {
		t.Fatalf("expiry = %v, want %v", sess.ExpiresAt, base.Add(time.Hour))
	}

	record, ok, err := store.lookup(ctx, sess.Token)
	if err != nil || !ok {
		t.Fatalf("lookup = %v, %v", ok, err)
	}
	if record.Username != "admin" || record.CSRFToken != sess.CSRFToken {
		t.Fatalf("record = %#v", record)
	}
}

func TestSessionStoreLookupMissing(t *testing.T) {
	store, _ := newSessionStore(testVault(t), time.Hour, fixedNow(time.Unix(0, 0)))
	_, ok, err := store.lookup(context.Background(), "unknown-token")
	if err != nil || ok {
		t.Fatalf("lookup missing = %v, %v", ok, err)
	}
}

func TestSessionStoreLookupExpiredDeletes(t *testing.T) {
	ctx := context.Background()
	clock := &mutableClock{now: time.Unix(1000, 0)}
	store, _ := newSessionStore(testVault(t), time.Minute, clock.Now)
	sess, _ := store.create(ctx, "admin")

	clock.now = clock.now.Add(2 * time.Minute)
	if _, ok, err := store.lookup(ctx, sess.Token); err != nil || ok {
		t.Fatalf("expired lookup = %v, %v", ok, err)
	}

	clock.now = time.Unix(1000, 0)
	if _, ok, _ := store.lookup(ctx, sess.Token); ok {
		t.Fatal("expired session should have been deleted")
	}
}

func TestSessionStoreDelete(t *testing.T) {
	ctx := context.Background()
	store, _ := newSessionStore(testVault(t), time.Hour, fixedNow(time.Unix(0, 0)))
	sess, _ := store.create(ctx, "admin")
	if err := store.delete(ctx, sess.Token); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, _ := store.lookup(ctx, sess.Token); ok {
		t.Fatal("deleted session should not be found")
	}
}

func TestNewSessionStoreRejectsDuplicate(t *testing.T) {
	storage := testVault(t)
	if _, err := newSessionStore(storage, time.Hour, fixedNow(time.Unix(0, 0))); err != nil {
		t.Fatalf("first newSessionStore: %v", err)
	}
	if _, err := newSessionStore(storage, time.Hour, fixedNow(time.Unix(0, 0))); err == nil {
		t.Fatal("second registration should fail")
	}
}

func TestSessionStoreCreateSurfacesTokenError(t *testing.T) {
	original := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }
	t.Cleanup(func() { randRead = original })

	store, _ := newSessionStore(testVault(t), time.Hour, fixedNow(time.Unix(0, 0)))
	if _, err := store.create(context.Background(), "admin"); err == nil {
		t.Fatal("create should fail when the session token random source fails")
	}
}

func TestSessionStoreCreateSurfacesCSRFError(t *testing.T) {
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

	store, _ := newSessionStore(testVault(t), time.Hour, fixedNow(time.Unix(0, 0)))
	if _, err := store.create(context.Background(), "admin"); err == nil {
		t.Fatal("create should fail when the csrf token random source fails")
	}
}

func TestSessionStoreCreateSurfacesPutError(t *testing.T) {
	engine := newScriptedEngine()
	engine.putErr = errors.New("disk full")
	store, _ := newSessionStore(scriptedVault(t, engine), time.Hour, fixedNow(time.Unix(0, 0)))
	if _, err := store.create(context.Background(), "admin"); err == nil {
		t.Fatal("create should surface the put error")
	}
}

func TestSessionStoreLookupSurfacesDecodeError(t *testing.T) {
	engine := newScriptedEngine()
	store, _ := newSessionStore(scriptedVault(t, engine), time.Hour, fixedNow(time.Unix(0, 0)))
	engine.buckets[adminSessionsBucket][hashToken("tok")] = []byte("{bad")
	if _, _, err := store.lookup(context.Background(), "tok"); err == nil {
		t.Fatal("lookup should surface a decode error")
	}
}

func TestSessionStoreLookupSurfacesExpiredDeleteError(t *testing.T) {
	ctx := context.Background()
	engine := newScriptedEngine()
	clock := &mutableClock{now: time.Unix(1000, 0)}
	store, _ := newSessionStore(scriptedVault(t, engine), time.Minute, clock.Now)
	sess, err := store.create(ctx, "admin")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	engine.deleteErr = errors.New("delete failed")
	clock.now = clock.now.Add(2 * time.Minute)
	if _, _, err := store.lookup(ctx, sess.Token); err == nil {
		t.Fatal("lookup should surface the delete error for an expired session")
	}
}

func TestSessionStoreDeleteSurfacesError(t *testing.T) {
	ctx := context.Background()
	engine := newScriptedEngine()
	store, _ := newSessionStore(scriptedVault(t, engine), time.Hour, fixedNow(time.Unix(0, 0)))
	sess, _ := store.create(ctx, "admin")
	engine.deleteErr = errors.New("delete failed")
	if err := store.delete(ctx, sess.Token); err == nil {
		t.Fatal("delete should surface the engine error")
	}
}
