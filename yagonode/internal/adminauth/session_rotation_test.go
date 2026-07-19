package adminauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestGuardRotatesDueSessionWithoutExtendingAbsoluteExpiry(t *testing.T) {
	clock := &mutableClock{now: time.Unix(10_000, 0)}
	service, err := New(testVault(t), Config{SessionTTL: 4 * time.Hour, Now: clock.Now})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	surface := guardedSurface(t, service)
	original, csrf := loginThroughGuard(t, surface)
	originalExpiry := original.Expires

	clock.now = clock.now.Add(time.Hour + time.Second)
	response := doRequest(surface, http.MethodGet, "/protected", "", original)
	if response.Code != http.StatusOK {
		t.Fatalf("rotated request status = %d", response.Code)
	}
	replacement := cookieNamed(response)
	if replacement == nil || replacement.Value == "" || replacement.Value == original.Value {
		t.Fatalf("replacement cookie = %#v", replacement)
	}
	if !replacement.Expires.Equal(originalExpiry) {
		t.Fatalf("replacement expiry = %v, want %v", replacement.Expires, originalExpiry)
	}
	if _, found, err := service.sessions.lookup(t.Context(), original.Value); err != nil || found {
		t.Fatalf("original session lookup = %t, %v", found, err)
	}
	record, found, err := service.sessions.lookup(t.Context(), replacement.Value)
	if err != nil || !found || record.CSRFToken != csrf {
		t.Fatalf("replacement session = %#v, %t, %v", record, found, err)
	}
	post := doRequestWithCSRF(surface, http.MethodPost, "/protected", csrf, replacement)
	if post.Code != http.StatusOK {
		t.Fatalf("rotated csrf status = %d", post.Code)
	}
}

func TestGuardKeepsSessionTokenBeforeRenewal(t *testing.T) {
	clock := &mutableClock{now: time.Unix(20_000, 0)}
	service, err := New(testVault(t), Config{SessionTTL: 4 * time.Hour, Now: clock.Now})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	surface := guardedSurface(t, service)
	cookie, _ := loginThroughGuard(t, surface)

	clock.now = clock.now.Add(59 * time.Minute)
	response := doRequest(surface, http.MethodGet, "/protected", "", cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("request status = %d", response.Code)
	}
	if replacement := cookieNamed(response); replacement != nil {
		t.Fatalf("unexpected replacement cookie = %#v", replacement)
	}
}

func TestGuardRotatesLegacySessionOnNextUse(t *testing.T) {
	clock := &mutableClock{now: time.Unix(30_000, 0)}
	service, err := New(testVault(t), Config{SessionTTL: 4 * time.Hour, Now: clock.Now})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	surface := guardedSurface(t, service)
	cookie, _ := loginThroughGuard(t, surface)

	if err := service.sessions.vault.Update(t.Context(), func(tx *vault.Txn) error {
		record, found, err := service.sessions.records.Get(tx, vault.Key(hashToken(cookie.Value)))
		if err != nil {
			return fmt.Errorf("read legacy session: %w", err)
		}
		if !found {
			return errors.New("legacy session is missing")
		}
		record.RenewAt = time.Time{}

		return service.sessions.records.Put(tx, vault.Key(hashToken(cookie.Value)), record)
	}); err != nil {
		t.Fatalf("store legacy session: %v", err)
	}

	response := doRequest(surface, http.MethodGet, "/protected", "", cookie)
	if response.Code != http.StatusOK || cookieNamed(response) == nil {
		t.Fatalf("legacy rotation status = %d, cookie = %#v", response.Code, cookieNamed(response))
	}
}

func TestGuardFailsClosedWhenRotationCannotPersist(t *testing.T) {
	clock := &mutableClock{now: time.Unix(40_000, 0)}
	engine := newScriptedEngine()
	service, err := New(scriptedVault(t, engine), Config{SessionTTL: 4 * time.Hour, Now: clock.Now})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	injectAdmin(t, engine, "admin", "pw")
	surface := guardedSurface(t, service)
	login := doRequest(surface, http.MethodPost, PathLogin, `{"username":"admin","password":"pw"}`)
	cookie := cookieNamed(login)
	clock.now = clock.now.Add(time.Hour + time.Second)
	engine.putErr = context.Canceled

	response := doRequest(surface, http.MethodGet, "/protected", "", cookie)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("rotation failure status = %d", response.Code)
	}
}

func TestSessionRotationRejectsExpiredAndMissingRecords(t *testing.T) {
	clock := &mutableClock{now: time.Unix(50_000, 0)}
	store, err := newSessionStore(testVault(t), time.Minute, clock.Now)
	if err != nil {
		t.Fatalf("new session store: %v", err)
	}
	created, err := store.create(t.Context(), "admin")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	record, found, err := store.lookup(t.Context(), created.Token)
	if err != nil || !found {
		t.Fatalf("lookup session = %#v, %t, %v", record, found, err)
	}
	clock.now = clock.now.Add(2 * time.Minute)
	if _, changed, err := store.rotate(t.Context(), created.Token, record); err != nil || changed {
		t.Fatalf("expired rotation = %t, %v", changed, err)
	}

	clock.now = time.Unix(60_000, 0)
	created, err = store.create(t.Context(), "admin")
	if err != nil {
		t.Fatalf("create replacement session: %v", err)
	}
	record, found, err = store.lookup(t.Context(), created.Token)
	if err != nil || !found {
		t.Fatalf("lookup replacement session = %#v, %t, %v", record, found, err)
	}
	if err := store.delete(t.Context(), created.Token); err != nil {
		t.Fatalf("delete replacement session: %v", err)
	}
	clock.now = clock.now.Add(31 * time.Second)
	if _, changed, err := store.rotate(t.Context(), created.Token, record); err != nil || changed {
		t.Fatalf("missing rotation = %t, %v", changed, err)
	}
}

func TestSessionRotationBoundsRenewalAndSurfacesEntropyFailure(t *testing.T) {
	clock := &mutableClock{now: time.Unix(70_000, 0)}
	store, err := newSessionStore(testVault(t), 30*time.Minute, clock.Now)
	if err != nil {
		t.Fatalf("new session store: %v", err)
	}
	created, err := store.create(t.Context(), "admin")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	record, found, err := store.lookup(t.Context(), created.Token)
	if err != nil || !found {
		t.Fatalf("lookup session = %#v, %t, %v", record, found, err)
	}
	clock.now = clock.now.Add(29 * time.Minute)
	rotated, changed, err := store.rotate(t.Context(), created.Token, record)
	if err != nil || !changed {
		t.Fatalf("bounded rotation = %#v, %t, %v", rotated, changed, err)
	}
	stored, found, err := store.lookup(t.Context(), rotated.Token)
	if err != nil || !found || !stored.RenewAt.Equal(stored.ExpiresAt) {
		t.Fatalf("bounded record = %#v, %t, %v", stored, found, err)
	}

	entropyClock := &mutableClock{now: time.Unix(75_000, 0)}
	entropyStore, err := newSessionStore(testVault(t), time.Hour, entropyClock.Now)
	if err != nil {
		t.Fatalf("new entropy session store: %v", err)
	}
	entropySession, err := entropyStore.create(t.Context(), "admin")
	if err != nil {
		t.Fatalf("create entropy session: %v", err)
	}
	entropyRecord, found, err := entropyStore.lookup(t.Context(), entropySession.Token)
	if err != nil || !found {
		t.Fatalf("lookup entropy session = %#v, %t, %v", entropyRecord, found, err)
	}
	entropyClock.now = entropyClock.now.Add(31 * time.Minute)
	original := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }
	t.Cleanup(func() { randRead = original })
	if _, changed, err = entropyStore.rotate(
		t.Context(), entropySession.Token, entropyRecord,
	); err == nil || changed {
		t.Fatalf("entropy failure rotation = %t, %v", changed, err)
	}
}

func TestSessionRotationSurfacesReadAndDeleteFailures(t *testing.T) {
	clock := &mutableClock{now: time.Unix(80_000, 0)}
	engine := newScriptedEngine()
	store, err := newSessionStore(scriptedVault(t, engine), time.Hour, clock.Now)
	if err != nil {
		t.Fatalf("new session store: %v", err)
	}
	created, err := store.create(t.Context(), "admin")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	record, found, err := store.lookup(t.Context(), created.Token)
	if err != nil || !found {
		t.Fatalf("lookup session = %#v, %t, %v", record, found, err)
	}
	clock.now = clock.now.Add(31 * time.Minute)
	engine.buckets[adminSessionsBucket][hashToken(created.Token)] = []byte("{bad")
	if _, changed, err := store.rotate(t.Context(), created.Token, record); err == nil || changed {
		t.Fatalf("read failure rotation = %t, %v", changed, err)
	}

	encoded, err := (sessionRecordCodec{}).Encode(record)
	if err != nil {
		t.Fatalf("encode session: %v", err)
	}
	engine.buckets[adminSessionsBucket][hashToken(created.Token)] = encoded
	engine.deleteErr = errors.New("delete failed")
	if _, changed, err := store.rotate(t.Context(), created.Token, record); err == nil || changed {
		t.Fatalf("delete failure rotation = %t, %v", changed, err)
	}
}
