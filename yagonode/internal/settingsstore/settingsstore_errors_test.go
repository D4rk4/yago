package settingsstore_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/settingsstore"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// fakeEngine is a minimal vault.Engine that can be pre-seeded with raw bytes and
// forced to fail individual bucket operations, to exercise the store's error
// branches that a healthy backend never triggers.
type fakeEngine struct {
	buckets  map[vault.Name]map[string][]byte
	failRead bool
	failPut  bool
	failDel  bool
	failScan bool
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{buckets: map[vault.Name]map[string][]byte{}}
}

func (e *fakeEngine) Provision(name vault.Name) error {
	if _, ok := e.buckets[name]; !ok {
		e.buckets[name] = map[string][]byte{}
	}

	return nil
}

func (e *fakeEngine) Update(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("scripted engine: %w", err)
	}

	return fn(&fakeTxn{engine: e, writable: true})
}

func (e *fakeEngine) View(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("scripted engine: %w", err)
	}

	return fn(&fakeTxn{engine: e, writable: false})
}

func (e *fakeEngine) Close() error                             { return nil }
func (e *fakeEngine) QuotaBytes() int64                        { return 0 }
func (e *fakeEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }

type fakeTxn struct {
	engine   *fakeEngine
	writable bool
}

func (t *fakeTxn) Writable() bool { return t.writable }

func (t *fakeTxn) Bucket(name vault.Name) vault.EngineBucket {
	entries := t.engine.buckets[name]
	if entries == nil {
		entries = map[string][]byte{}
		t.engine.buckets[name] = entries
	}

	return &fakeBucket{engine: t.engine, entries: entries}
}

type fakeBucket struct {
	engine  *fakeEngine
	entries map[string][]byte
}

func (b *fakeBucket) Get(key vault.Key) []byte { return b.entries[string(key)] }

func (b *fakeBucket) ReadValue(key vault.Key) ([]byte, bool, error) {
	if b.engine.failRead {
		return nil, false, errors.New("read failed")
	}
	value, found := b.entries[string(key)]

	return value, found, nil
}

func (b *fakeBucket) Put(key vault.Key, val []byte) error {
	if b.engine.failPut {
		return errors.New("put failed")
	}
	stored := make([]byte, len(val))
	copy(stored, val)
	b.entries[string(key)] = stored

	return nil
}

func (b *fakeBucket) Delete(key vault.Key) error {
	if b.engine.failDel {
		return errors.New("delete failed")
	}
	delete(b.entries, string(key))

	return nil
}

func (b *fakeBucket) Scan(prefix vault.Key, fn func(vault.Key, []byte) (bool, error)) error {
	if b.engine.failScan {
		return errors.New("scan failed")
	}
	keys := make([]string, 0, len(b.entries))
	for key := range b.entries {
		if strings.HasPrefix(key, string(prefix)) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		keep, err := fn(vault.Key(key), b.entries[key])
		if err != nil {
			return err
		}
		if !keep {
			return nil
		}
	}

	return nil
}

func fakeStore(t *testing.T, engine *fakeEngine) *settingsstore.Store {
	t.Helper()

	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	store, err := settingsstore.Open(v)
	if err != nil {
		t.Fatalf("settingsstore.Open: %v", err)
	}

	return store
}

func TestOpenRegisterErrorClosedVault(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	if err := v.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := settingsstore.Open(v); err == nil {
		t.Fatal("Open on a closed vault must fail to register")
	}
}

func TestGetOuterErrorCanceledContext(t *testing.T) {
	store := openTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := store.Get(ctx, "portal.enabled"); err == nil {
		t.Fatal("Get with a canceled context must fail")
	}
}

func TestGetReadError(t *testing.T) {
	engine := newFakeEngine()
	engine.failRead = true
	store := fakeStore(t, engine)
	if _, _, err := store.Get(context.Background(), "portal.enabled"); err == nil {
		t.Fatal("Get must fail when the underlying read fails")
	}
}

func TestSetPutError(t *testing.T) {
	engine := newFakeEngine()
	engine.failPut = true
	store := fakeStore(t, engine)
	if err := store.Set(context.Background(), "portal.enabled", "true"); err == nil {
		t.Fatal("Set must fail when the underlying put fails")
	}
}

func TestUnsetDeleteError(t *testing.T) {
	engine := newFakeEngine()
	engine.failDel = true
	engine.buckets["runtime_settings"] = map[string][]byte{"portal.enabled": []byte("t")}
	store := fakeStore(t, engine)
	if err := store.Unset(context.Background(), "portal.enabled"); err == nil {
		t.Fatal("Unset must fail when the underlying delete fails")
	}
}

func TestAllScanError(t *testing.T) {
	engine := newFakeEngine()
	engine.failScan = true
	store := fakeStore(t, engine)
	if _, err := store.All(context.Background()); err == nil {
		t.Fatal("All must fail when the underlying scan fails")
	}
}
