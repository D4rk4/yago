package peerbirth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/vault"
)

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

func TestOpenKeepsBirthDateAcrossRestarts(t *testing.T) {
	engine := newStubEngine()
	first := time.Date(2026, 7, 2, 10, 0, 0, 500, time.UTC)

	birth, err := Open(
		context.Background(),
		openStubVault(t, engine),
		func() time.Time { return first },
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if want := first.Truncate(time.Second); !birth.Equal(want) {
		t.Fatalf("birth = %v, want %v", birth, want)
	}

	later := first.Add(48 * time.Hour)
	reopened, err := Open(
		context.Background(),
		openStubVault(t, engine),
		func() time.Time { return later },
	)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if !reopened.Equal(birth) {
		t.Fatalf("reopened birth = %v, want stored %v", reopened, birth)
	}
}

func TestOpenRejectsSecondRegistration(t *testing.T) {
	storage := openStubVault(t, newStubEngine())
	now := func() time.Time { return time.Unix(100, 0) }

	if _, err := Open(context.Background(), storage, now); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := Open(context.Background(), storage, now); err == nil {
		t.Fatal("second Open on one vault did not fail")
	}
}

func TestOpenRejectsCorruptBirthDate(t *testing.T) {
	engine := newStubEngine()
	if err := engine.Provision(birthBucket); err != nil {
		t.Fatalf("provision: %v", err)
	}
	engine.buckets[birthBucket][string(birthKey)] = []byte("not-a-timestamp")

	_, err := Open(
		context.Background(),
		openStubVault(t, engine),
		func() time.Time { return time.Unix(100, 0) },
	)
	if err == nil {
		t.Fatal("corrupt stored birth date did not fail")
	}
}

func TestOpenReportsStoreFailure(t *testing.T) {
	engine := newStubEngine()
	engine.putError = errors.New("disk full")

	_, err := Open(
		context.Background(),
		openStubVault(t, engine),
		func() time.Time { return time.Unix(100, 0) },
	)
	if err == nil {
		t.Fatal("store failure did not fail")
	}
}
