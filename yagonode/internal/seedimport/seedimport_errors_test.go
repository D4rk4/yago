package seedimport_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/seedimport"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// fakeEngine is a minimal vault.Engine that can be pre-seeded with raw bytes and
// forced to fail a put, to exercise the store's error branches a healthy backend
// never triggers.
type fakeEngine struct {
	buckets map[vault.Name]map[string][]byte
	failPut bool
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
	delete(b.entries, string(key))

	return nil
}

func (b *fakeBucket) Scan(vault.Key, func(vault.Key, []byte) (bool, error)) error {
	return nil
}

func fakeStore(t *testing.T, engine *fakeEngine) *seedimport.Store {
	t.Helper()
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	store, err := seedimport.Open(v, func() time.Time { return time.Unix(100, 0) })
	if err != nil {
		t.Fatalf("seedimport.Open: %v", err)
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
	if _, err := seedimport.Open(v, time.Now); err == nil {
		t.Fatal("Open on a closed vault should fail")
	}
}

func TestRecordReturnsPutError(t *testing.T) {
	engine := newFakeEngine()
	store := fakeStore(t, engine)
	engine.failPut = true

	if err := store.Record(context.Background(), "https://x/", 1, nil); err == nil {
		t.Fatal("Record should surface a storage put failure")
	}
}

func TestGetReturnsDecodeError(t *testing.T) {
	engine := newFakeEngine()
	store := fakeStore(t, engine)
	engine.buckets["seedimport-status"] = map[string][]byte{"https://x/": []byte("not-json")}

	if _, _, err := store.Get(context.Background(), "https://x/"); err == nil {
		t.Fatal("Get should surface a decode failure for a corrupt record")
	}
}

func TestGetReturnsViewError(t *testing.T) {
	store := fakeStore(t, newFakeEngine())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, err := store.Get(ctx, "https://x/"); err == nil {
		t.Fatal("Get should surface a view failure")
	}
}
