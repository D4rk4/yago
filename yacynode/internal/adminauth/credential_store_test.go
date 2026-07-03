package adminauth

import (
	"context"
	"errors"
	"testing"
)

func TestCredentialStoreLifecycle(t *testing.T) {
	ctx := context.Background()
	store, err := newCredentialStore(testVault(t))
	if err != nil {
		t.Fatalf("newCredentialStore: %v", err)
	}

	present, err := store.exists(ctx)
	if err != nil || present {
		t.Fatalf("exists on empty store = %v, %v", present, err)
	}

	if err := store.createIfAbsent(ctx, "admin", "s3cret"); err != nil {
		t.Fatalf("createIfAbsent: %v", err)
	}
	present, err = store.exists(ctx)
	if err != nil || !present {
		t.Fatalf("exists after create = %v, %v", present, err)
	}
	if err := store.createIfAbsent(ctx, "admin", "other"); !errors.Is(err, errAdminExists) {
		t.Fatalf("second createIfAbsent = %v, want errAdminExists", err)
	}

	ok, err := store.verify(ctx, "admin", "s3cret")
	if err != nil || !ok {
		t.Fatalf("verify correct = %v, %v", ok, err)
	}
	ok, err = store.verify(ctx, "admin", "wrong")
	if err != nil || ok {
		t.Fatalf("verify wrong password = %v, %v", ok, err)
	}
	ok, err = store.verify(ctx, "intruder", "s3cret")
	if err != nil || ok {
		t.Fatalf("verify wrong username = %v, %v", ok, err)
	}
}

func TestCredentialStoreVerifyMissingAdmin(t *testing.T) {
	store, err := newCredentialStore(testVault(t))
	if err != nil {
		t.Fatalf("newCredentialStore: %v", err)
	}
	ok, err := store.verify(context.Background(), "admin", "whatever")
	if err != nil || ok {
		t.Fatalf("verify with no admin = %v, %v", ok, err)
	}
}

func TestCredentialStoreSetAdminReplaces(t *testing.T) {
	ctx := context.Background()
	store, _ := newCredentialStore(testVault(t))
	if err := store.setAdmin(ctx, "admin", "first"); err != nil {
		t.Fatalf("setAdmin first: %v", err)
	}
	if err := store.setAdmin(ctx, "admin", "second"); err != nil {
		t.Fatalf("setAdmin second: %v", err)
	}
	if ok, _ := store.verify(ctx, "admin", "second"); !ok {
		t.Fatal("replaced password should verify")
	}
	if ok, _ := store.verify(ctx, "admin", "first"); ok {
		t.Fatal("old password should no longer verify")
	}
}

func TestNewCredentialStoreRejectsDuplicate(t *testing.T) {
	storage := testVault(t)
	if _, err := newCredentialStore(storage); err != nil {
		t.Fatalf("first newCredentialStore: %v", err)
	}
	if _, err := newCredentialStore(storage); err == nil {
		t.Fatal("second registration should fail")
	}
}

func TestCredentialStoreExistsSurfacesDecodeError(t *testing.T) {
	engine := newScriptedEngine()
	store, err := newCredentialStore(scriptedVault(t, engine))
	if err != nil {
		t.Fatalf("newCredentialStore: %v", err)
	}
	engine.buckets[adminCredentialsBucket][string(adminKey)] = []byte("{not json")
	if _, err := store.exists(context.Background()); err == nil {
		t.Fatal("exists should surface a decode error")
	}
}

func TestCredentialStoreVerifySurfacesDecodeError(t *testing.T) {
	engine := newScriptedEngine()
	store, _ := newCredentialStore(scriptedVault(t, engine))
	engine.buckets[adminCredentialsBucket][string(adminKey)] = []byte("{not json")
	if _, err := store.verify(context.Background(), "admin", "x"); err == nil {
		t.Fatal("verify should surface a decode error")
	}
}

func TestCredentialStoreVerifySurfacesBadStoredHash(t *testing.T) {
	engine := newScriptedEngine()
	store, _ := newCredentialStore(scriptedVault(t, engine))
	engine.buckets[adminCredentialsBucket][string(adminKey)] = []byte(
		`{"username":"admin","passwordHash":"garbage"}`,
	)
	if _, err := store.verify(context.Background(), "admin", "x"); err == nil {
		t.Fatal("verify should surface a malformed stored hash")
	}
}

func TestCredentialStoreCreateIfAbsentSurfacesExistsError(t *testing.T) {
	engine := newScriptedEngine()
	store, _ := newCredentialStore(scriptedVault(t, engine))
	engine.buckets[adminCredentialsBucket][string(adminKey)] = []byte("{not json")
	if err := store.createIfAbsent(context.Background(), "admin", "pw"); err == nil {
		t.Fatal("createIfAbsent should surface the exists error")
	}
}

func TestCredentialStoreSetAdminSurfacesPutError(t *testing.T) {
	engine := newScriptedEngine()
	engine.putErr = errors.New("disk full")
	store, _ := newCredentialStore(scriptedVault(t, engine))
	if err := store.setAdmin(context.Background(), "admin", "pw"); err == nil {
		t.Fatal("setAdmin should surface the put error")
	}
}

func TestCredentialStoreSetAdminSurfacesHashError(t *testing.T) {
	original := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }
	t.Cleanup(func() { randRead = original })

	store, _ := newCredentialStore(testVault(t))
	if err := store.setAdmin(context.Background(), "admin", "pw"); err == nil {
		t.Fatal("setAdmin should surface the hashing error")
	}
}
