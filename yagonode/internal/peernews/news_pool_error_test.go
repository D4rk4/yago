package peernews

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type newsStubEngine struct {
	buckets      map[vault.Name]map[string][]byte
	putErrors    map[vault.Name]error
	deleteErrors map[vault.Name]error
	scanErrors   map[vault.Name]error
}

func newNewsStubEngine() *newsStubEngine {
	return &newsStubEngine{
		buckets:      map[vault.Name]map[string][]byte{},
		putErrors:    map[vault.Name]error{},
		deleteErrors: map[vault.Name]error{},
		scanErrors:   map[vault.Name]error{},
	}
}

func (e *newsStubEngine) Update(_ context.Context, fn func(vault.EngineTxn) error) error {
	return fn(newsStubTxn{engine: e, writable: true})
}

func (e *newsStubEngine) View(_ context.Context, fn func(vault.EngineTxn) error) error {
	return fn(newsStubTxn{engine: e})
}

func (e *newsStubEngine) Provision(name vault.Name) error {
	if e.buckets[name] == nil {
		e.buckets[name] = map[string][]byte{}
	}

	return nil
}

func (e *newsStubEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }
func (e *newsStubEngine) QuotaBytes() int64                        { return 0 }
func (e *newsStubEngine) Close() error                             { return nil }

type newsStubTxn struct {
	engine   *newsStubEngine
	writable bool
}

func (t newsStubTxn) Bucket(name vault.Name) vault.EngineBucket {
	return newsStubBucket{engine: t.engine, name: name}
}

func (t newsStubTxn) Writable() bool { return t.writable }

type newsStubBucket struct {
	engine *newsStubEngine
	name   vault.Name
}

func (b newsStubBucket) Get(key vault.Key) []byte {
	raw := b.engine.buckets[b.name][string(key)]
	if raw == nil {
		return nil
	}

	return append([]byte(nil), raw...)
}

func (b newsStubBucket) Put(key vault.Key, raw []byte) error {
	if err := b.engine.putErrors[b.name]; err != nil {
		return err
	}
	b.engine.buckets[b.name][string(key)] = append([]byte(nil), raw...)

	return nil
}

func (b newsStubBucket) Delete(key vault.Key) error {
	if err := b.engine.deleteErrors[b.name]; err != nil {
		return err
	}
	delete(b.engine.buckets[b.name], string(key))

	return nil
}

func (b newsStubBucket) Scan(prefix vault.Key, fn func(vault.Key, []byte) (bool, error)) error {
	if err := b.engine.scanErrors[b.name]; err != nil {
		return err
	}
	keys := make([]string, 0, len(b.engine.buckets[b.name]))
	for key := range b.engine.buckets[b.name] {
		if bytes.HasPrefix([]byte(key), prefix) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	for _, key := range keys {
		again, err := fn(vault.Key(key), append([]byte(nil), b.engine.buckets[b.name][key]...))
		if err != nil || !again {
			return err
		}
	}

	return nil
}

func openStubPool(t *testing.T, engine *newsStubEngine) *Pool {
	t.Helper()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	pool, err := Open(storage, fixedNow)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	return pool
}

func TestOpenRejectsDuplicateBuckets(t *testing.T) {
	for _, bucket := range []vault.Name{queueBucket, knownBucket, cursorBucket} {
		storage, err := vault.New(newNewsStubEngine())
		if err != nil {
			t.Fatalf("vault.New: %v", err)
		}
		if _, err := vault.Register(storage, bucket, wireCodec{}); err != nil {
			t.Fatalf("pre-register %s: %v", bucket, err)
		}
		if _, err := Open(storage, fixedNow); err == nil {
			t.Errorf("Open with taken bucket %s did not fail", bucket)
		}
	}
}

func TestPublishOwnNewsReturnsStorageErrors(t *testing.T) {
	ctx := context.Background()
	originator := yagomodel.WordHash("myseed")

	knownBroken := newNewsStubEngine()
	knownBroken.putErrors[knownBucket] = errors.New("known put failed")
	if err := openStubPool(
		t,
		knownBroken,
	).PublishOwnNews(ctx, originator, "TestCat", nil); err == nil {
		t.Error("known put failure did not fail")
	}

	queueBroken := newNewsStubEngine()
	queueBroken.putErrors[queueBucket] = errors.New("queue put failed")
	if err := openStubPool(
		t,
		queueBroken,
	).PublishOwnNews(ctx, originator, "TestCat", nil); err == nil {
		t.Error("queue put failure did not fail")
	}

	cursorBroken := newNewsStubEngine()
	cursorBroken.putErrors[cursorBucket] = errors.New("cursor put failed")
	if err := openStubPool(
		t,
		cursorBroken,
	).PublishOwnNews(ctx, originator, "TestCat", nil); err == nil {
		t.Error("cursor put failure did not fail")
	}

	knownCorrupt := newNewsStubEngine()
	pool := openStubPool(t, knownCorrupt)
	if err := pool.PublishOwnNews(ctx, originator, "TestCat", nil); err != nil {
		t.Fatalf("PublishOwnNews: %v", err)
	}
	for key := range knownCorrupt.buckets[knownBucket] {
		knownCorrupt.buckets[knownBucket][key] = []byte("corrupt")
	}
	if err := pool.PublishOwnNews(ctx, originator, "TestCat", nil); err == nil {
		t.Error("corrupt known marker did not fail")
	}

	cursorCorrupt := newNewsStubEngine()
	pool = openStubPool(t, cursorCorrupt)
	cursorCorrupt.buckets[cursorBucket][string(Incoming)] = []byte("not-a-number")
	if err := pool.PublishOwnNews(ctx, originator, "TestCat", nil); err == nil {
		t.Error("corrupt cursor did not fail")
	}
}

func TestNextPublicationReturnsStorageErrors(t *testing.T) {
	ctx := context.Background()
	originator := yagomodel.WordHash("myseed")

	scanBroken := newNewsStubEngine()
	pool := openStubPool(t, scanBroken)
	if err := pool.PublishOwnNews(ctx, originator, "TestCat", nil); err != nil {
		t.Fatalf("PublishOwnNews: %v", err)
	}
	scanBroken.scanErrors[queueBucket] = errors.New("scan failed")
	if _, _, err := pool.NextPublication(ctx); err == nil {
		t.Error("scan failure did not fail")
	}

	deleteBroken := newNewsStubEngine()
	pool = openStubPool(t, deleteBroken)
	if err := pool.PublishOwnNews(ctx, originator, "TestCat", nil); err != nil {
		t.Fatalf("PublishOwnNews: %v", err)
	}
	deleteBroken.deleteErrors[queueBucket] = errors.New("delete failed")
	if _, _, err := pool.NextPublication(ctx); err == nil {
		t.Error("delete failure did not fail")
	}

	corrupt := newNewsStubEngine()
	pool = openStubPool(t, corrupt)
	corrupt.buckets[queueBucket][string(queueKey(Outgoing, 1))] = []byte("{dis=many}")
	if _, _, err := pool.NextPublication(ctx); err == nil {
		t.Error("corrupt stored record did not fail")
	}
}

func TestEnqueueIncomingNewsReturnsStorageErrors(t *testing.T) {
	ctx := context.Background()
	record := Record{
		Originator: yagomodel.WordHash("peer"),
		Created:    fixedNow(),
		Category:   CategoryCrawlStart,
	}

	broken := newNewsStubEngine()
	broken.putErrors[knownBucket] = errors.New("known put failed")
	if _, err := openStubPool(t, broken).EnqueueIncomingNews(ctx, record); err == nil {
		t.Error("known put failure did not fail")
	}

	corrupt := newNewsStubEngine()
	pool := openStubPool(t, corrupt)
	if _, err := pool.EnqueueIncomingNews(ctx, record); err != nil {
		t.Fatalf("EnqueueIncomingNews: %v", err)
	}
	for key := range corrupt.buckets[knownBucket] {
		corrupt.buckets[knownBucket][key] = []byte("corrupt")
	}
	if _, err := pool.EnqueueIncomingNews(ctx, record); err == nil {
		t.Error("corrupt known marker did not fail")
	}
}

func TestByIDReturnsStorageErrors(t *testing.T) {
	ctx := context.Background()

	scanBroken := newNewsStubEngine()
	pool := openStubPool(t, scanBroken)
	scanBroken.scanErrors[queueBucket] = errors.New("scan failed")
	if _, _, err := pool.ByID(ctx, Published, "x"); err == nil {
		t.Error("scan failure did not fail")
	}

	corrupt := newNewsStubEngine()
	pool = openStubPool(t, corrupt)
	corrupt.buckets[queueBucket][string(queueKey(Published, 1))] = []byte("{dis=many}")
	if _, _, err := pool.ByID(ctx, Published, "x"); err == nil {
		t.Error("corrupt stored record did not fail")
	}
}

func TestRecentReturnsStorageErrors(t *testing.T) {
	ctx := context.Background()

	scanBroken := newNewsStubEngine()
	pool := openStubPool(t, scanBroken)
	scanBroken.scanErrors[queueBucket] = errors.New("scan failed")
	if _, err := pool.Recent(ctx, Incoming, 5); err == nil {
		t.Error("scan failure did not fail")
	}

	corrupt := newNewsStubEngine()
	pool = openStubPool(t, corrupt)
	corrupt.buckets[queueBucket][string(queueKey(Incoming, 1))] = []byte("{dis=many}")
	if _, err := pool.Recent(ctx, Incoming, 5); err == nil {
		t.Error("corrupt stored record did not fail")
	}
}

func TestNewsPoolSurvivesReopenOnSameStorage(t *testing.T) {
	ctx := context.Background()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	if err := pool.PublishOwnNews(ctx, yagomodel.WordHash("myseed"), "TestCat", nil); err != nil {
		t.Fatalf("PublishOwnNews: %v", err)
	}

	reopened := openStubPool(t, engine)
	record, ok, err := reopened.NextPublication(ctx)
	if err != nil || !ok {
		t.Fatalf("NextPublication after reopen = %v, %v", ok, err)
	}
	if record.Distributed != 1 {
		t.Fatalf("distributed = %d, want 1", record.Distributed)
	}
}
