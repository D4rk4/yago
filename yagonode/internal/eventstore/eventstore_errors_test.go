package eventstore_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/eventstore"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// fakeEngine is a minimal vault.Engine that can be pre-seeded with raw bytes and
// forced to fail individual bucket operations, to exercise the store's error
// branches that a healthy backend never triggers.
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

func badTimeEvent() events.Event {
	return events.Event{
		Time:     time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC),
		Severity: events.SeverityInfo,
		Category: events.CategoryConfig,
		Name:     "bad",
		Message:  "bad",
	}
}

func TestOpenWithCapacityZeroUsesDefault(t *testing.T) {
	store, err := eventstore.OpenWithCapacity(context.Background(), testVault(t), 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if store == nil {
		t.Fatal("store = nil")
	}
}

func TestOpenRegisterErrorOnClosedVault(t *testing.T) {
	v := testVault(t)
	if err := v.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := eventstore.Open(context.Background(), v); err == nil {
		t.Fatal("Open on a closed vault must fail to register")
	}
}

func TestOpenResumeCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := eventstore.Open(ctx, testVault(t)); err == nil {
		t.Fatal("Open with a canceled context must fail to resume")
	}
}

func TestAppendEncodeError(t *testing.T) {
	ctx := context.Background()
	store, err := eventstore.Open(ctx, testVault(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := store.Append(ctx, badTimeEvent()); err == nil {
		t.Fatal("Append of an unencodable event must fail")
	}
}

func TestRecentDecodeError(t *testing.T) {
	ctx := context.Background()
	engine := newFakeEngine()
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	store, err := eventstore.Open(ctx, v)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	engine.buckets["events"]["corrupt"] = []byte("not json")
	if _, err := store.Recent(ctx); err == nil {
		t.Fatal("Recent over corrupt bytes must fail to decode")
	}
}

func TestAppendPruneDeleteError(t *testing.T) {
	ctx := context.Background()
	engine := newFakeEngine()
	engine.failDel = true
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	store, err := eventstore.OpenWithCapacity(ctx, v, 1)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ev := event("a")
	if err := store.Append(ctx, ev); err != nil {
		t.Fatalf("append first: %v", err)
	}
	if err := store.Append(ctx, ev); err == nil {
		t.Fatal("Append that prunes must fail when the delete fails")
	}
}
