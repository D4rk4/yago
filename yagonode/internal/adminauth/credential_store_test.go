package adminauth

import (
	"context"
	"errors"
	"slices"
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

// TestCredentialStoreVerifyEqualizesWorkForUnknownAccounts proves a failed
// verification spends its Argon2 work whether or not the account exists: a
// missing record and a username that does not match both hash the presented
// password against the fixed placeholder, and only a matching username is
// checked against the stored hash. Returning early instead would make the login
// response fast exactly when the account is absent, turning response timing into
// an account-existence oracle for anyone probing the endpoint.
func TestCredentialStoreVerifyEqualizesWorkForUnknownAccounts(t *testing.T) {
	const storedHash = "argon2id-hash-of-the-real-operator-password"
	original := credentialPasswordVerify
	presented := make([]string, 0, 3)
	credentialPasswordVerify = func(encoded, password string) (bool, error) {
		presented = append(presented, encoded)

		return encoded == storedHash && password == "correct-horse", nil
	}
	t.Cleanup(func() { credentialPasswordVerify = original })

	ctx := context.Background()
	engine := newScriptedEngine()
	store, err := newCredentialStore(scriptedVault(t, engine))
	if err != nil {
		t.Fatalf("newCredentialStore: %v", err)
	}
	if ok, err := store.verify(ctx, "operator", "correct-horse"); err != nil || ok {
		t.Fatalf("verify with no admin record = %v, %v", ok, err)
	}
	injectRawAdmin(t, engine, "operator", storedHash)
	if ok, err := store.verify(ctx, "intruder", "correct-horse"); err != nil || ok {
		t.Fatalf("verify with a mismatched username = %v, %v", ok, err)
	}
	if ok, err := store.verify(ctx, "operator", "correct-horse"); err != nil || !ok {
		t.Fatalf("verify with the stored username = %v, %v", ok, err)
	}
	want := []string{dummyPasswordHash, dummyPasswordHash, storedHash}
	if !slices.Equal(presented, want) {
		t.Fatalf("hashes verified = %v, want the placeholder twice then the stored hash", presented)
	}
}

// TestDummyPasswordHashCostsWhatAStoredHashCosts proves the timing-equalization
// placeholder is as expensive as a real credential. It is only useful if it
// drives the same Argon2 parameters and key length as hashPassword; a cheaper
// placeholder would leave the account-existence timing difference it exists to
// erase.
func TestDummyPasswordHashCostsWhatAStoredHashCosts(t *testing.T) {
	params, salt, key, err := decodeArgon2id(dummyPasswordHash)
	if err != nil {
		t.Fatalf("decode placeholder hash: %v", err)
	}
	want := argon2Params{
		memory:      argonMemoryKiB,
		iterations:  argonIterations,
		parallelism: argonParallelism,
	}
	if params != want {
		t.Fatalf("placeholder params = %+v, want %+v", params, want)
	}
	if len(salt) != argonSaltLength || len(key) != argonKeyLength {
		t.Fatalf("placeholder salt/key = %d/%d bytes", len(salt), len(key))
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
