package urldenylist_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/urldenylist"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// fakeEngine is a minimal vault.Engine whose bucket operations can be forced to
// fail, exercising the store's error branches a healthy backend never triggers.
type fakeEngine struct {
	buckets  map[vault.Name]map[string][]byte
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

func (b *fakeBucket) Scan(_ vault.Key, fn func(vault.Key, []byte) (bool, error)) error {
	if b.engine.failScan {
		return errors.New("scan failed")
	}
	for key, val := range b.entries {
		keep, err := fn(vault.Key(key), val)
		if err != nil {
			return err
		}
		if !keep {
			return nil
		}
	}

	return nil
}

func fakeStore(t *testing.T, engine *fakeEngine) *urldenylist.Store {
	t.Helper()
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	store, err := urldenylist.Open(v, func() time.Time { return time.Unix(200, 0) })
	if err != nil {
		t.Fatalf("urldenylist.Open: %v", err)
	}

	return store
}

func TestOpenReturnsRegisterError(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	if err := v.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := urldenylist.Open(v, time.Now); err == nil {
		t.Fatal("Open on a closed vault should fail")
	}
}

func TestAddReturnsPutError(t *testing.T) {
	engine := newFakeEngine()
	store := fakeStore(t, engine)
	engine.failPut = true

	if err := store.Add(context.Background(), urldenylist.KindDomain, "example.com"); err == nil {
		t.Fatal("Add should surface a storage put failure")
	}
}

func TestRemoveReturnsDeleteError(t *testing.T) {
	engine := newFakeEngine()
	store := fakeStore(t, engine)
	// The delete path is only reached for a key that exists, so seed one first.
	engine.buckets["urldenylist"] = map[string][]byte{"domain\x00example.com": []byte("{}")}
	engine.failDel = true

	if _, err := store.Remove(
		context.Background(),
		urldenylist.KindDomain,
		"example.com",
	); err == nil {
		t.Fatal("Remove should surface a storage delete failure")
	}
}

func TestEntriesReturnsScanError(t *testing.T) {
	engine := newFakeEngine()
	store := fakeStore(t, engine)
	engine.failScan = true

	if _, err := store.Entries(context.Background()); err == nil {
		t.Fatal("Entries should surface a scan failure")
	}
}

func TestSnapshotReturnsScanError(t *testing.T) {
	engine := newFakeEngine()
	store := fakeStore(t, engine)
	engine.failScan = true

	if _, err := store.Snapshot(context.Background()); err == nil {
		t.Fatal("Snapshot should surface a scan failure")
	}
}

func TestEntriesReturnsDecodeError(t *testing.T) {
	engine := newFakeEngine()
	store := fakeStore(t, engine)
	engine.buckets["urldenylist"] = map[string][]byte{"domain\x00bad.example": []byte("not-json")}

	if _, err := store.Entries(context.Background()); err == nil {
		t.Fatal("Entries should surface a decode failure for a corrupt record")
	}
}

func TestSnapshotReturnsDecodeError(t *testing.T) {
	engine := newFakeEngine()
	store := fakeStore(t, engine)
	engine.buckets["urldenylist"] = map[string][]byte{
		"url\x00https://bad.example/": []byte("not-json"),
	}

	if _, err := store.Snapshot(context.Background()); err == nil {
		t.Fatal("Snapshot should surface a decode failure for a corrupt record")
	}
}
