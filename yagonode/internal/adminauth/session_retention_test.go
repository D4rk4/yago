package adminauth

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestSessionStoreBoundsActiveSessionsByEvictingOldest(t *testing.T) {
	ctx := context.Background()
	clock := &mutableClock{now: time.Unix(1000, 0)}
	store, err := newSessionStore(testVault(t), 24*time.Hour, clock.Now)
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	sessions := make([]session, 0, maximumAdminSessions+1)
	for range maximumAdminSessions + 1 {
		created, createErr := store.create(ctx, "admin")
		if createErr != nil {
			t.Fatalf("create: %v", createErr)
		}
		sessions = append(sessions, created)
		clock.now = clock.now.Add(time.Second)
	}
	if length := sessionStoreLength(t, store); length != maximumAdminSessions {
		t.Fatalf("session records = %d, want %d", length, maximumAdminSessions)
	}
	if _, found, lookupErr := store.lookup(ctx, sessions[0].Token); lookupErr != nil || found {
		t.Fatalf("oldest lookup = %v, %v", found, lookupErr)
	}
	if _, found, lookupErr := store.lookup(
		ctx,
		sessions[len(sessions)-1].Token,
	); lookupErr != nil || !found {
		t.Fatalf("newest lookup = %v, %v", found, lookupErr)
	}
}

func TestSessionStorePurgesExpiredSessionsDuringCreate(t *testing.T) {
	ctx := context.Background()
	clock := &mutableClock{now: time.Unix(1000, 0)}
	store, err := newSessionStore(testVault(t), time.Minute, clock.Now)
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	expired, err := store.create(ctx, "admin")
	if err != nil {
		t.Fatalf("create expired candidate: %v", err)
	}
	clock.now = clock.now.Add(2 * time.Minute)
	if _, err := store.create(ctx, "admin"); err != nil {
		t.Fatalf("create replacement: %v", err)
	}
	if length := sessionStoreLength(t, store); length != 1 {
		t.Fatalf("session records = %d, want 1", length)
	}
	if _, found, lookupErr := store.lookup(ctx, expired.Token); lookupErr != nil || found {
		t.Fatalf("expired lookup = %v, %v", found, lookupErr)
	}
}

func TestSessionStoreCreateSurfacesRetentionScanError(t *testing.T) {
	engine := newScriptedEngine()
	store, err := newSessionStore(
		scriptedVault(t, engine),
		time.Hour,
		fixedNow(time.Unix(1000, 0)),
	)
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	engine.buckets[adminSessionsBucket]["corrupt"] = []byte("{")
	if _, err := store.create(context.Background(), "admin"); err == nil {
		t.Fatal("create should surface the retention scan error")
	}
}

func TestSessionStoreCreateSurfacesRetentionDeleteError(t *testing.T) {
	ctx := context.Background()
	engine := newScriptedEngine()
	clock := &mutableClock{now: time.Unix(1000, 0)}
	store, err := newSessionStore(scriptedVault(t, engine), time.Minute, clock.Now)
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	if _, err := store.create(ctx, "admin"); err != nil {
		t.Fatalf("create: %v", err)
	}
	clock.now = clock.now.Add(2 * time.Minute)
	engine.deleteErr = errors.New("locked")
	if _, err := store.create(ctx, "admin"); err == nil {
		t.Fatal("create should surface the retention delete error")
	}
}

func sessionStoreLength(t *testing.T, store *sessionStore) int {
	t.Helper()
	length := 0
	if err := store.vault.View(context.Background(), func(tx *vault.Txn) error {
		var err error
		length, err = store.records.Len(tx)
		if err != nil {
			return fmt.Errorf("measure session records: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("session length: %v", err)
	}

	return length
}
