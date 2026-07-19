package contentcluster

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

var (
	errInjectedClusterVault         = errors.New("injected content cluster vault failure")
	errInjectedRelaxedClusterCommit = errors.New("injected relaxed cluster commit failure")
)

type clusterFaultEngine struct {
	mu               sync.RWMutex
	buckets          map[vault.Name]map[string][]byte
	provisionFailure vault.Name
	putFailure       vault.Name
	deleteFailure    vault.Name
	updates          int
	replayUpdate     func(*clusterFaultEngine)
	partialUpdate    int
	partialAfter     int
	readGateBucket   vault.Name
	readGateKey      string
	readGateEntered  chan struct{}
	readGateRelease  <-chan struct{}
}

type clusterFaultTxn struct {
	engine   *clusterFaultEngine
	buckets  map[vault.Name]map[string][]byte
	writable bool
	touched  map[int]struct{}
}

type clusterFaultBucket struct {
	transaction *clusterFaultTxn
	name        vault.Name
}

func newClusterFaultEngine() *clusterFaultEngine {
	return &clusterFaultEngine{
		buckets:      make(map[vault.Name]map[string][]byte),
		partialAfter: -1,
	}
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
	e.updates++
	replay := e.replayUpdate
	e.replayUpdate = nil
	if replay != nil {
		staged := cloneClusterBuckets(e.buckets)
		transaction := &clusterFaultTxn{
			engine:   e,
			buckets:  staged,
			writable: true,
			touched:  make(map[int]struct{}),
		}
		if err := fn(transaction); err != nil {
			return err
		}
		replay(e)
	}
	staged := cloneClusterBuckets(e.buckets)
	transaction := &clusterFaultTxn{
		engine:   e,
		buckets:  staged,
		writable: true,
		touched:  make(map[int]struct{}),
	}
	if err := fn(transaction); err != nil {
		return err
	}
	if e.partialUpdate == e.updates {
		e.applyRelaxedCommit(staged, transaction.touched, e.partialAfter)
		e.partialUpdate = 0
		e.partialAfter = -1

		return errInjectedRelaxedClusterCommit
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

	return fn(&clusterFaultTxn{engine: e, buckets: e.buckets, touched: make(map[int]struct{})})
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

func (e *clusterFaultEngine) loseShard(shard int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for name, bucket := range e.buckets {
		for key := range bucket {
			if e.route(name, vault.Key(key)) == shard {
				delete(bucket, key)
			}
		}
	}
}

func (e *clusterFaultEngine) failRelaxedUpdateAfter(offset int, committedShards int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.partialUpdate = e.updates + offset
	e.partialAfter = committedShards
}

func (e *clusterFaultEngine) applyRelaxedCommit(
	staged map[vault.Name]map[string][]byte,
	touched map[int]struct{},
	committedShards int,
) {
	shards := make([]int, 0, len(touched))
	for shard := range touched {
		shards = append(shards, shard)
	}
	sort.Ints(shards)
	committedShards = min(max(committedShards, 0), len(shards))
	shards = shards[:committedShards]
	for _, shard := range shards {
		e.applyRelaxedShard(staged, shard)
	}
}

func (e *clusterFaultEngine) applyRelaxedShard(
	staged map[vault.Name]map[string][]byte,
	shard int,
) {
	for name, bucket := range staged {
		if _, exists := e.buckets[name]; !exists {
			e.buckets[name] = make(map[string][]byte)
		}
		for key := range e.buckets[name] {
			if e.route(name, vault.Key(key)) == shard {
				delete(e.buckets[name], key)
			}
		}
		for key, raw := range bucket {
			if e.route(name, vault.Key(key)) == shard {
				e.buckets[name][key] = append([]byte(nil), raw...)
			}
		}
	}
}

func (e *clusterFaultEngine) route(name vault.Name, key vault.Key) int {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(name))
	_, _ = hasher.Write(key)
	routes := [...]int{0, 1, 2, 3, 4, 5, 6, 7}

	return routes[hasher.Sum32()&7]
}

func (t *clusterFaultTxn) Bucket(name vault.Name) vault.EngineBucket {
	return &clusterFaultBucket{transaction: t, name: name}
}

func (t *clusterFaultTxn) Writable() bool {
	return t.writable
}

func (b *clusterFaultBucket) Get(key vault.Key) []byte {
	if b.name == b.transaction.engine.readGateBucket &&
		string(key) == b.transaction.engine.readGateKey {
		select {
		case b.transaction.engine.readGateEntered <- struct{}{}:
		default:
		}
		<-b.transaction.engine.readGateRelease
	}

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
	b.transaction.touched[b.transaction.engine.route(b.name, key)] = struct{}{}

	return nil
}

func (b *clusterFaultBucket) Delete(key vault.Key) error {
	if b.name == b.transaction.engine.deleteFailure {
		return errInjectedClusterVault
	}
	delete(b.transaction.buckets[b.name], string(key))
	b.transaction.touched[b.transaction.engine.route(b.name, key)] = struct{}{}

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

func reopenFaultIndex(
	t *testing.T,
	engine *clusterFaultEngine,
) *Index {
	t.Helper()
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("reopen fault vault: %v", err)
	}
	index, err := Open(v, Limits{})
	if err != nil {
		t.Fatalf("reopen fault index: %v", err)
	}

	return index
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
