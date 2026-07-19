package remotecrawl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

var errRemoteCrawlStorageFault = errors.New("remote crawl storage fault")

type remoteCrawlFaultEngine struct {
	mu               sync.RWMutex
	buckets          map[vault.Name]map[string][]byte
	provisionFailure vault.Name
	putFailure       vault.Name
	deleteFailure    vault.Name
	scanFailure      vault.Name
	viewFailure      bool
	updateFailure    bool
}

type remoteCrawlFaultTransaction struct {
	engine   *remoteCrawlFaultEngine
	buckets  map[vault.Name]map[string][]byte
	writable bool
}

type remoteCrawlFaultBucket struct {
	transaction *remoteCrawlFaultTransaction
	name        vault.Name
}

func newRemoteCrawlFaultEngine() *remoteCrawlFaultEngine {
	return &remoteCrawlFaultEngine{buckets: make(map[vault.Name]map[string][]byte)}
}

func (e *remoteCrawlFaultEngine) Provision(name vault.Name) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if name == e.provisionFailure {
		return errRemoteCrawlStorageFault
	}
	if _, found := e.buckets[name]; !found {
		e.buckets[name] = make(map[string][]byte)
	}

	return nil
}

func (e *remoteCrawlFaultEngine) Update(
	ctx context.Context,
	change func(vault.EngineTxn) error,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("check remote crawl fault update context: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.updateFailure {
		return errRemoteCrawlStorageFault
	}
	staged := cloneRemoteCrawlBuckets(e.buckets)
	transaction := &remoteCrawlFaultTransaction{
		engine: e, buckets: staged, writable: true,
	}
	if err := change(transaction); err != nil {
		return err
	}
	e.buckets = staged

	return nil
}

func (e *remoteCrawlFaultEngine) View(
	ctx context.Context,
	read func(vault.EngineTxn) error,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("check remote crawl fault view context: %w", err)
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.viewFailure {
		return errRemoteCrawlStorageFault
	}

	return read(&remoteCrawlFaultTransaction{engine: e, buckets: e.buckets})
}

func (e *remoteCrawlFaultEngine) UsedBytes(context.Context) (int64, error) {
	return 0, nil
}

func (e *remoteCrawlFaultEngine) QuotaBytes() int64 {
	return 0
}

func (e *remoteCrawlFaultEngine) Close() error {
	return nil
}

func (e *remoteCrawlFaultEngine) putRaw(name vault.Name, key vault.Key, raw []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, found := e.buckets[name]; !found {
		e.buckets[name] = make(map[string][]byte)
	}
	e.buckets[name][string(key)] = append([]byte(nil), raw...)
}

func (e *remoteCrawlFaultEngine) deleteRaw(name vault.Name, key vault.Key) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.buckets[name], string(key))
}

func (t *remoteCrawlFaultTransaction) Bucket(name vault.Name) vault.EngineBucket {
	return &remoteCrawlFaultBucket{transaction: t, name: name}
}

func (t *remoteCrawlFaultTransaction) Writable() bool {
	return t.writable
}

func (b *remoteCrawlFaultBucket) Get(key vault.Key) []byte {
	return append([]byte(nil), b.transaction.buckets[b.name][string(key)]...)
}

func (b *remoteCrawlFaultBucket) Put(key vault.Key, raw []byte) error {
	if b.name == b.transaction.engine.putFailure {
		return errRemoteCrawlStorageFault
	}
	if _, found := b.transaction.buckets[b.name]; !found {
		b.transaction.buckets[b.name] = make(map[string][]byte)
	}
	b.transaction.buckets[b.name][string(key)] = append([]byte(nil), raw...)

	return nil
}

func (b *remoteCrawlFaultBucket) Delete(key vault.Key) error {
	if b.name == b.transaction.engine.deleteFailure {
		return errRemoteCrawlStorageFault
	}
	delete(b.transaction.buckets[b.name], string(key))

	return nil
}

func (b *remoteCrawlFaultBucket) Scan(
	prefix vault.Key,
	visit func(vault.Key, []byte) (bool, error),
) error {
	if b.name == b.transaction.engine.scanFailure {
		return errRemoteCrawlStorageFault
	}
	keys := make([]string, 0, len(b.transaction.buckets[b.name]))
	for key := range b.transaction.buckets[b.name] {
		if bytes.HasPrefix([]byte(key), prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		keepGoing, err := visit(
			vault.Key(key),
			append([]byte(nil), b.transaction.buckets[b.name][key]...),
		)
		if err != nil || !keepGoing {
			return err
		}
	}

	return nil
}

func cloneRemoteCrawlBuckets(
	source map[vault.Name]map[string][]byte,
) map[vault.Name]map[string][]byte {
	cloned := make(map[vault.Name]map[string][]byte, len(source))
	for name, bucket := range source {
		cloned[name] = make(map[string][]byte, len(bucket))
		for key, raw := range bucket {
			cloned[name][key] = append([]byte(nil), raw...)
		}
	}

	return cloned
}

func openRemoteCrawlFaultBroker(
	t *testing.T,
) (*Broker, *vault.Vault, *remoteCrawlFaultEngine) {
	t.Helper()
	engine := newRemoteCrawlFaultEngine()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	broker, err := Open(remoteConfig(time.Now), storage, &recordingReceiver{})
	if err != nil {
		t.Fatal(err)
	}

	return broker, storage, engine
}

func registerRemoteCrawlFaultCollections(
	t *testing.T,
) (collections, *vault.Vault, *remoteCrawlFaultEngine) {
	t.Helper()
	engine := newRemoteCrawlFaultEngine()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	registered, err := registerCollections(storage)
	if err != nil {
		t.Fatal(err)
	}

	return registered, storage, engine
}

func TestRemoteCrawlCollectionRegistrationReportsEveryStorageFailure(t *testing.T) {
	for _, bucket := range []vault.Name{
		remoteCrawlOrderBucket,
		remoteCrawlURLSequenceBucket,
		remoteCrawlSequenceBucket,
		remoteCrawlRequestRateBucket,
		remoteCrawlLeaseCountBucket,
		remoteCrawlLeaseExpiryBucket,
		remoteCrawlPendingBucket,
		remoteCrawlSchemaBucket,
	} {
		t.Run(string(bucket), func(t *testing.T) {
			engine := newRemoteCrawlFaultEngine()
			storage, err := vault.New(engine)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = storage.Close() })
			engine.provisionFailure = bucket
			if _, err := registerCollections(storage); err == nil {
				t.Fatal("registration succeeded")
			}
		})
	}
}

func TestOpenRemoteCrawlReportsRegistrationAndReconciliationFailures(t *testing.T) {
	t.Run("registration", func(t *testing.T) {
		engine := newRemoteCrawlFaultEngine()
		storage, err := vault.New(engine)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = storage.Close() })
		engine.provisionFailure = remoteCrawlOrderBucket
		if _, err := Open(
			remoteConfig(time.Now),
			storage,
			&recordingReceiver{},
		); err == nil {
			t.Fatal("registration failure ignored")
		}
	})
	t.Run("reconciliation", func(t *testing.T) {
		engine := newRemoteCrawlFaultEngine()
		storage, err := vault.New(engine)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = storage.Close() })
		engine.putRaw(remoteCrawlSchemaBucket, queueStateVersionKey, []byte{1})
		if _, err := Open(
			remoteConfig(time.Now),
			storage,
			&recordingReceiver{},
		); err == nil {
			t.Fatal("reconciliation failure ignored")
		}
	})
}
