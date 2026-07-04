package rankingprofile

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// fakeEngine is a minimal vault.Engine that can be pre-seeded with raw bytes and
// forced to fail individual bucket operations, to exercise the holder's error
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

func TestEncodeErrorForNonFiniteWeight(t *testing.T) {
	if _, err := (weightsCodec{}).Encode(
		searchindex.RankingWeights{Title: math.Inf(1)},
	); err == nil {
		t.Fatal("Encode of a non-finite weight must fail")
	}
}

func TestDecodeErrorForCorruptBytes(t *testing.T) {
	if _, err := (weightsCodec{}).Decode([]byte("not json")); err == nil {
		t.Fatal("Decode of corrupt bytes must fail")
	}
}

func TestOpenRegisterErrorClosedVault(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	if err := v.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := Open(context.Background(), v); err == nil {
		t.Fatal("Open on a closed vault must fail to register")
	}
}

func TestOpenLoadCorruptBytes(t *testing.T) {
	engine := newFakeEngine()
	engine.buckets[profileBucket] = map[string][]byte{string(profileKey): []byte("not json")}
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	if _, err := Open(context.Background(), v); err == nil {
		t.Fatal("Open over a corrupt stored profile must fail to load")
	}
}

func TestCurrentReturnsDefaultForNilPointer(t *testing.T) {
	holder := &Holder{}
	if holder.Current() != searchindex.DefaultRankingWeights() {
		t.Fatalf("current = %+v, want default", holder.Current())
	}
}

func TestSetPutError(t *testing.T) {
	engine := newFakeEngine()
	engine.failPut = true
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	holder, err := Open(context.Background(), v)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	valid := searchindex.RankingWeights{Title: 1, Headings: 1, Anchors: 1, Body: 1, URL: 1}
	if err := holder.Set(context.Background(), valid); err == nil {
		t.Fatal("Set must fail when the underlying put fails")
	}
}
