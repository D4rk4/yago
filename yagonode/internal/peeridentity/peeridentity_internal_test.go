package peeridentity

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// --- stub vault engine (mirrors the peerbirth test harness) ---

type stubEngine struct {
	buckets  map[vault.Name]map[string][]byte
	putError error
}

func newStubEngine() *stubEngine {
	return &stubEngine{buckets: map[vault.Name]map[string][]byte{}}
}

func (e *stubEngine) Update(_ context.Context, fn func(vault.EngineTxn) error) error {
	return fn(stubTxn{engine: e, writable: true})
}

func (e *stubEngine) View(_ context.Context, fn func(vault.EngineTxn) error) error {
	return fn(stubTxn{engine: e})
}

func (e *stubEngine) Provision(name vault.Name) error {
	if e.buckets[name] == nil {
		e.buckets[name] = map[string][]byte{}
	}

	return nil
}

func (e *stubEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }
func (e *stubEngine) QuotaBytes() int64                        { return 0 }
func (e *stubEngine) Close() error                             { return nil }

type stubTxn struct {
	engine   *stubEngine
	writable bool
}

func (t stubTxn) Bucket(name vault.Name) vault.EngineBucket {
	return stubBucket{engine: t.engine, name: name}
}

func (t stubTxn) Writable() bool { return t.writable }

type stubBucket struct {
	engine *stubEngine
	name   vault.Name
}

func (b stubBucket) Get(key vault.Key) []byte {
	raw := b.engine.buckets[b.name][string(key)]
	if raw == nil {
		return nil
	}

	return append([]byte(nil), raw...)
}

func (b stubBucket) Put(key vault.Key, raw []byte) error {
	if b.engine.putError != nil {
		return b.engine.putError
	}
	b.engine.buckets[b.name][string(key)] = append([]byte(nil), raw...)

	return nil
}

func (b stubBucket) Delete(key vault.Key) error {
	delete(b.engine.buckets[b.name], string(key))

	return nil
}

func (b stubBucket) Scan(vault.Key, func(vault.Key, []byte) (bool, error)) error {
	return nil
}

func openStubVault(t *testing.T, engine *stubEngine) *vault.Vault {
	t.Helper()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}

	return storage
}

// --- helpers ---

func mustHash(t *testing.T, s string) yagomodel.Hash {
	t.Helper()
	hash, err := yagomodel.ParseHash(s)
	if err != nil {
		t.Fatalf("ParseHash(%q): %v", s, err)
	}

	return hash
}

func fixedGen(hash yagomodel.Hash, name string) Generators {
	return Generators{
		Hash: func() (yagomodel.Hash, error) { return hash, nil },
		Name: func() (string, error) { return name, nil },
	}
}

func failingGen() Generators {
	return Generators{
		Hash: func() (yagomodel.Hash, error) { return "", errors.New("no hash") },
		Name: func() (string, error) { return "", errors.New("no name") },
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

// --- tests ---

func TestOpenUsesDeclaredIdentityAndPersistsIt(t *testing.T) {
	engine := newStubEngine()
	declaredHash := mustHash(t, "0123456789AB")

	// A declared hash and name win and are recorded; generators are not called.
	hash, name, err := Open(
		context.Background(), openStubVault(t, engine), declaredHash, "operator", failingGen(),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if hash != declaredHash || name != "operator" {
		t.Fatalf("identity = (%q,%q), want the declared values", hash, name)
	}

	// Reopening without any declared values reuses the persisted identity, so it
	// stays stable even after the environment variables are unset.
	reHash, reName, err := Open(
		context.Background(), openStubVault(t, engine), "", "", failingGen(),
	)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reHash != declaredHash || reName != "operator" {
		t.Fatalf("reopened identity = (%q,%q), want the stored values", reHash, reName)
	}
}

func TestOpenGeneratesAndKeepsIdentity(t *testing.T) {
	engine := newStubEngine()
	generated := mustHash(t, "CDEFGHIJKLMN")

	hash, name, err := Open(
		context.Background(), openStubVault(t, engine), "", "", fixedGen(generated, "yago-fixed"),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if hash != generated || name != "yago-fixed" {
		t.Fatalf("identity = (%q,%q), want the generated values", hash, name)
	}

	// A later start reuses the stored identity rather than generating a new one.
	reHash, reName, err := Open(
		context.Background(), openStubVault(t, engine), "", "", failingGen(),
	)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reHash != generated || reName != "yago-fixed" {
		t.Fatalf("reopened identity = (%q,%q), want the stored values", reHash, reName)
	}
}

func TestOpenRejectsSecondRegistration(t *testing.T) {
	storage := openStubVault(t, newStubEngine())
	gen := fixedGen(mustHash(t, "0123456789AB"), "yago-x")

	if _, _, err := Open(context.Background(), storage, "", "", gen); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, _, err := Open(context.Background(), storage, "", "", gen); err == nil {
		t.Fatal("second Open on one vault did not fail")
	}
}

func TestOpenRejectsCorruptStoredIdentity(t *testing.T) {
	for name, raw := range map[string]string{
		"no separator": "0123456789AB",
		"bad hash":     "short" + recordSeparator + "yago-x",
	} {
		t.Run(name, func(t *testing.T) {
			engine := newStubEngine()
			if err := engine.Provision(identityBucket); err != nil {
				t.Fatalf("provision: %v", err)
			}
			engine.buckets[identityBucket][string(identityKey)] = []byte(raw)

			_, _, err := Open(
				context.Background(), openStubVault(t, engine), "", "", failingGen(),
			)
			if err == nil {
				t.Fatal("corrupt stored identity did not fail")
			}
		})
	}
}

func TestOpenReportsStoreFailure(t *testing.T) {
	engine := newStubEngine()
	engine.putError = errors.New("disk full")

	_, _, err := Open(
		context.Background(),
		openStubVault(t, engine),
		"", "",
		fixedGen(mustHash(t, "0123456789AB"), "yago-x"),
	)
	if err == nil {
		t.Fatal("store failure did not fail")
	}
}

func TestOpenReportsHashGeneratorFailure(t *testing.T) {
	_, _, err := Open(
		context.Background(), openStubVault(t, newStubEngine()), "", "", failingGen(),
	)
	if err == nil {
		t.Fatal("hash generator failure did not fail")
	}
}

func TestOpenReportsNameGeneratorFailure(t *testing.T) {
	// A declared hash makes hash resolution succeed, so the failing name
	// generator is what surfaces.
	gen := Generators{
		Hash: func() (yagomodel.Hash, error) { return "", errors.New("unused") },
		Name: func() (string, error) { return "", errors.New("no name") },
	}
	_, _, err := Open(
		context.Background(),
		openStubVault(t, newStubEngine()),
		mustHash(t, "0123456789AB"),
		"",
		gen,
	)
	if err == nil {
		t.Fatal("name generator failure did not fail")
	}
}

func TestGenerateName(t *testing.T) {
	name, err := GenerateName(bytes.NewReader([]byte{0xDE, 0xAD, 0xBE, 0xEF}))
	if err != nil {
		t.Fatalf("GenerateName: %v", err)
	}
	if want := peerNamePrefix + "deadbeef"; name != want {
		t.Fatalf("name = %q, want %q", name, want)
	}

	if _, err := GenerateName(failingReader{}); err == nil {
		t.Fatal("expected an entropy read error")
	}
}

func TestNewNameAndDefaultGenerators(t *testing.T) {
	name, err := NewName()
	if err != nil {
		t.Fatalf("NewName: %v", err)
	}
	if !strings.HasPrefix(name, peerNamePrefix) {
		t.Fatalf("name = %q, want the %q prefix", name, peerNamePrefix)
	}

	gen := DefaultGenerators()
	hash, err := gen.Hash()
	if err != nil {
		t.Fatalf("default hash generator: %v", err)
	}
	if len(hash) != yagomodel.HashLength {
		t.Fatalf("generated hash %q has length %d, want %d", hash, len(hash), yagomodel.HashLength)
	}
	if _, err := gen.Name(); err != nil {
		t.Fatalf("default name generator: %v", err)
	}
}
