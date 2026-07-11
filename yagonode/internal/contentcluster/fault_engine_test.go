package contentcluster

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

var errInjectedClusterVault = errors.New("injected content cluster vault failure")

type clusterFaultEngine struct {
	mu               sync.RWMutex
	buckets          map[vault.Name]map[string][]byte
	provisionFailure vault.Name
	putFailure       vault.Name
	deleteFailure    vault.Name
}

type clusterFaultTxn struct {
	engine   *clusterFaultEngine
	buckets  map[vault.Name]map[string][]byte
	writable bool
}

type clusterFaultBucket struct {
	transaction *clusterFaultTxn
	name        vault.Name
}

func newClusterFaultEngine() *clusterFaultEngine {
	return &clusterFaultEngine{buckets: make(map[vault.Name]map[string][]byte)}
}

func (e *clusterFaultEngine) Provision(name vault.Name) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if name == e.provisionFailure {
		return errInjectedClusterVault
	}
	if _, exists := e.buckets[name]; !exists {
		e.buckets[name] = make(map[string][]byte)
	}

	return nil
}

func (e *clusterFaultEngine) Update(
	ctx context.Context,
	fn func(vault.EngineTxn) error,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("check fault update context: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	staged := cloneClusterBuckets(e.buckets)
	transaction := &clusterFaultTxn{engine: e, buckets: staged, writable: true}
	if err := fn(transaction); err != nil {
		return err
	}
	e.buckets = staged

	return nil
}

func (e *clusterFaultEngine) View(
	ctx context.Context,
	fn func(vault.EngineTxn) error,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("check fault view context: %w", err)
	}
	e.mu.RLock()
	defer e.mu.RUnlock()

	return fn(&clusterFaultTxn{engine: e, buckets: e.buckets})
}

func (e *clusterFaultEngine) UsedBytes(context.Context) (int64, error) {
	return 0, nil
}

func (e *clusterFaultEngine) QuotaBytes() int64 {
	return 0
}

func (e *clusterFaultEngine) Close() error {
	return nil
}

func (e *clusterFaultEngine) putRaw(name vault.Name, key vault.Key, raw []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, exists := e.buckets[name]; !exists {
		e.buckets[name] = make(map[string][]byte)
	}
	e.buckets[name][string(key)] = append([]byte(nil), raw...)
}

func (e *clusterFaultEngine) deleteRaw(name vault.Name, key vault.Key) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.buckets[name], string(key))
}

func (t *clusterFaultTxn) Bucket(name vault.Name) vault.EngineBucket {
	return &clusterFaultBucket{transaction: t, name: name}
}

func (t *clusterFaultTxn) Writable() bool {
	return t.writable
}

func (b *clusterFaultBucket) Get(key vault.Key) []byte {
	return append([]byte(nil), b.transaction.buckets[b.name][string(key)]...)
}

func (b *clusterFaultBucket) Put(key vault.Key, raw []byte) error {
	if b.name == b.transaction.engine.putFailure {
		return errInjectedClusterVault
	}
	if _, exists := b.transaction.buckets[b.name]; !exists {
		b.transaction.buckets[b.name] = make(map[string][]byte)
	}
	b.transaction.buckets[b.name][string(key)] = append([]byte(nil), raw...)

	return nil
}

func (b *clusterFaultBucket) Delete(key vault.Key) error {
	if b.name == b.transaction.engine.deleteFailure {
		return errInjectedClusterVault
	}
	delete(b.transaction.buckets[b.name], string(key))

	return nil
}

func (b *clusterFaultBucket) Scan(
	prefix vault.Key,
	visit func(vault.Key, []byte) (bool, error),
) error {
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

func cloneClusterBuckets(
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

func openFaultIndex(t *testing.T, limits Limits) (*Index, *clusterFaultEngine) {
	t.Helper()
	engine := newClusterFaultEngine()
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("new fault vault: %v", err)
	}
	index, err := Open(v, limits)
	if err != nil {
		t.Fatalf("open fault index: %v", err)
	}

	return index, engine
}

type stagedCancellationContext struct {
	context.Context
	cancelAt int
	calls    int
}

func (c *stagedCancellationContext) Err() error {
	c.calls++
	if c.calls >= c.cancelAt {
		return context.Canceled
	}

	return nil
}
