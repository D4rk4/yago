// Package shardvault is the sharded, compressed bbolt implementation of the
// vault Engine (ADR-0025). Records route to one of N independent bbolt files
// by a hash of bucket and key, each shard living at vault/aa/bb/cc/aabbcc.vlt;
// values compress with zstd at the fastest level. Each shard is its own
// failure domain: losing one file loses 1/N of the keyspace, never the store.
// Cross-shard atomicity is relaxed by design — an Update commits each touched
// shard independently, and callers order their writes so a crash leaves
// re-ingestable partial state.
package shardvault

import (
	"bytes"
	"container/heap"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cespare/xxhash/v2"
	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	minShards = 8
	maxShards = 1024
)

// shardBytesTarget is the per-shard live-byte target that drives automatic
// growth: the pool splits while the mean shard load exceeds it (ADR-0037). It is
// a var so a test can lower it to force growth without writing gigabytes.
var shardBytesTarget = int64(7) << 30

var newVault = vault.New

// commitTx and closeDB are seams so tests can exercise commit and close
// failures.
var (
	commitTx = (*bolt.Tx).Commit
	closeDB  = (*bolt.DB).Close
	openBolt = bolt.Open
)

// Open opens (or creates) the sharded vault rooted at dir with the given
// quota. The shard count derives from the quota and is recorded in the layout
// manifest on first open; later opens reuse the recorded count.
func Open(dir string, quotaBytes int64) (*vault.Vault, error) {
	shardEngine, err := openEngine(dir, quotaBytes)
	if err != nil {
		return nil, err
	}

	return vaultOverEngine(shardEngine)
}

func vaultOverEngine(shardEngine *engine) (*vault.Vault, error) {
	vaulted, err := newVault(shardEngine)
	if err != nil {
		closeShards(shardEngine.shards)

		return nil, fmt.Errorf("initialize sharded storage: %w", err)
	}

	return vaulted, nil
}

// openEngine opens the shard files behind the manifest-recorded layout.
func openEngine(dir string, quotaBytes int64) (*engine, error) {
	// New vaults start at the concurrency floor and grow with the data (ADR-0037);
	// the quota no longer sizes the layout.
	manifest, err := loadOrCreateManifest(dir, minShards)
	if err != nil {
		return nil, err
	}
	count := manifest.shardCount()
	shards := make([]*bolt.DB, count)
	for i := range shards {
		path := shardPath(dir, i)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			closeShards(shards)

			return nil, fmt.Errorf("create shard directory: %w", err)
		}
		db, err := openOrQuarantineShard(path, i)
		if err != nil {
			closeShards(shards)

			return nil, err
		}
		shards[i] = db
	}

	e := &engine{
		shards: shards,
		dir:    dir,
		level:  manifest.Level,
		split:  manifest.Split,
		// Sized to the cap so a split never grows it: a []sync.Mutex cannot be
		// appended without copying locks. Indices past the live count stay unused
		// until the pool grows into them (ADR-0037).
		shardLocks: make([]sync.Mutex, maxShards),
	}
	e.quotaBytes.Store(quotaBytes)

	return e, nil
}

// quarantineSuffix marks a shard file set aside after failing to open; the
// store keeps serving the surviving shards (ADR-0025 loss tolerance).
const quarantineSuffix = ".quarantine"

// openOrQuarantineShard opens one shard file; a shard that cannot open is
// quarantined (renamed aside for offline inspection) and replaced with a
// fresh empty shard, so a single damaged file costs 1/N of the keyspace
// instead of the store.
func openOrQuarantineShard(path string, shard int) (*bolt.DB, error) {
	db, err := openBolt(path, 0o600, openTimeoutOptions())
	if err == nil {
		return db, nil
	}
	slog.WarnContext(context.Background(), "vault shard quarantined",
		slog.Int("shard", shard),
		slog.String("path", path),
		slog.Any("error", err),
	)
	if renameErr := os.Rename(path, path+quarantineSuffix); renameErr != nil {
		return nil, fmt.Errorf("quarantine shard %d: %w", shard, renameErr)
	}
	db, err = openBolt(path, 0o600, openTimeoutOptions())
	if err != nil {
		return nil, fmt.Errorf("recreate quarantined shard %d: %w", shard, err)
	}

	return db, nil
}

// openTimeoutOptions bounds the file-lock wait so a stuck lock surfaces as an
// error instead of hanging startup, and trims per-commit write volume for
// fsync-bound storage (IO-AGG-03): the freelist is not persisted on every
// commit (NoFreelistSync — bbolt rebuilds it on open by scanning the file) and
// is held as a hashmap, the faster shape for write-heavy shards.
func openTimeoutOptions() *bolt.Options {
	return &bolt.Options{
		Timeout:        5 * time.Second,
		NoFreelistSync: true,
		FreelistType:   bolt.FreelistMapType,
	}
}

// shardPath is the three-level fanout location of one shard file.
func shardPath(dir string, shard int) string {
	id := fmt.Sprintf("%06x", shard)

	return filepath.Join(dir, id[0:2], id[2:4], id[4:6], id+".vlt")
}

func closeShards(shards []*bolt.DB) {
	for _, db := range shards {
		if db != nil {
			_ = db.Close()
		}
	}
}

type engine struct {
	shards []*bolt.DB
	dir    string
	// quotaBytes is the live disk-budget ceiling. It is atomic so the admin
	// console can raise or lower it without a restart (ADR-0037 D): AtCapacity
	// and the eviction sweep read it each cycle.
	quotaBytes atomic.Int64
	// level and split are the linear-hashing state (ADR-0037): the pool holds
	// 2^level + split shards and route reads them to place a record. They change
	// only under the exclusive globalGate (a split), so a gate-holding reader
	// always sees a consistent pair.
	level int
	split int
	// Writers hold shardLocks for the shards they touch, so updates landing on
	// disjoint shards commit concurrently (PERF-06). bbolt allows one write
	// transaction per file, and lazily opening several shards from concurrent
	// updates in data-driven order can deadlock — a writer that fails to take a
	// shard lock therefore rolls back, upgrades to the exclusive globalGate,
	// and retries alone. Fast-path writers hold the gate shared. Inserts into
	// one collection still serialize on its length-counter shard (the counter
	// key is the collection name), so the win is between different collections
	// and on update-in-place writes.
	shardLocks []sync.Mutex
	globalGate sync.RWMutex
}

// errShardContended aborts a fast-path update whose next shard is locked by a
// concurrent writer; the update retries under the exclusive gate.
var errShardContended = fmt.Errorf("shard contended: %w", vault.ErrContended)

// route picks the shard for one record under the current linear-hashing state.
func (e *engine) route(bucket vault.Name, key vault.Key) int {
	hash := xxhash.New()
	_, _ = hash.WriteString(string(bucket))
	_, _ = hash.Write(key)

	return e.locate(hash.Sum64())
}

// locate maps a record hash to its shard for the linear-hashing state
// (level, split): buckets below the split pointer have been rehashed under the
// wider mask, the rest still use the level mask. With split == 0 this is exactly
// hash mod 2^level — the pre-ADR-0037 routing — so an unsplit pool is unchanged.
func (e *engine) locate(sum uint64) int {
	wide := sum & (uint64(1)<<(e.level+1) - 1)
	full := int(wide) //nolint:gosec // wide < 2^(level+1) ≤ 2·maxShards.
	half := full & (1<<e.level - 1)
	if half >= e.split {
		return half
	}

	return full
}

func (e *engine) Provision(name vault.Name) error {
	e.globalGate.RLock()
	defer e.globalGate.RUnlock()

	for i, db := range e.shards {
		err := db.Update(func(tx *bolt.Tx) error {
			if _, createErr := tx.CreateBucketIfNotExists([]byte(name)); createErr != nil {
				return fmt.Errorf("create bucket: %w", createErr)
			}

			return nil
		})
		if err != nil {
			return fmt.Errorf("provision bucket %s on shard %d: %w", name, i, err)
		}
	}

	return nil
}

// Update runs fn over a lazy multi-shard transaction: a shard's write
// transaction opens on first touch and every opened transaction commits when
// fn succeeds (or rolls back when it fails). Commits are per shard — the
// relaxed atomicity ADR-0025 documents. Updates touching disjoint shards run
// concurrently; on shard contention fn is rolled back and re-run once under
// the exclusive gate, so fn must not leak side effects before it returns.
func (e *engine) Update(_ context.Context, fn func(vault.EngineTxn) error) error {
	e.globalGate.RLock()
	err := e.runUpdate(fn, true)
	e.globalGate.RUnlock()
	if !errors.Is(err, errShardContended) {
		return err
	}

	e.globalGate.Lock()
	defer e.globalGate.Unlock()

	return e.runUpdate(fn, false)
}

// runUpdate executes one update attempt; tryLocks selects the fast path that
// takes per-shard locks and aborts on contention.
func (e *engine) runUpdate(fn func(vault.EngineTxn) error, tryLocks bool) error {
	txn := &shardTxn{
		engine:   e,
		writable: true,
		tryLocks: tryLocks,
		open:     make([]*bolt.Tx, len(e.shards)),
	}
	defer txn.releaseLocks()
	if err := fn(txn); err != nil {
		txn.rollback()
		if errors.Is(err, errShardContended) {
			return errShardContended
		}
		if storageAtCapacityError(err) {
			return vault.ErrAtCapacity
		}

		return err
	}
	if err := txn.commit(); err != nil {
		if storageAtCapacityError(err) {
			return vault.ErrAtCapacity
		}

		return fmt.Errorf("update storage: %w", err)
	}

	return nil
}

func (e *engine) View(_ context.Context, fn func(vault.EngineTxn) error) error {
	// Readers hold the gate shared so a compaction swap (exclusive) waits for
	// in-flight reads to finish and no read starts mid-swap. The rollback defer
	// is declared last so it runs before the gate is released.
	e.globalGate.RLock()
	defer e.globalGate.RUnlock()

	txn := &shardTxn{engine: e, open: make([]*bolt.Tx, len(e.shards))}
	defer txn.rollback()
	if err := fn(txn); err != nil {
		return err
	}

	return nil
}

// UsedBytes reports the live data held across the shards, excluding bbolt free
// pages, mirroring boltvault. A shard file grows to its high-water mark and
// bbolt never returns freed pages to the OS, so the raw file size overstates
// usage after churn (deletes, recrawl re-ingest, eviction). The quota and the
// eviction sweep that loops on this figure must see the bytes actually in use,
// not the peak file size — otherwise the sweep can never drop below the mark
// and thrashes. Compaction (the periodic maintenance pass) is what returns the
// freed pages to the OS and shrinks the files.
func (e *engine) UsedBytes(_ context.Context) (int64, error) {
	// Shared gate: block only against a compaction swap (exclusive), never
	// against other reads or writes.
	e.globalGate.RLock()
	defer e.globalGate.RUnlock()

	var total int64
	for i, db := range e.shards {
		live, err := liveBytes(db)
		if err != nil {
			return 0, fmt.Errorf("measure shard %d: %w", i, err)
		}
		total += live
	}

	return total, nil
}

// liveBytes returns one shard's in-use bytes: the file size minus the free and
// pending free pages the freelist can reuse.
func liveBytes(db *bolt.DB) (int64, error) {
	size, free, err := shardSizeAndFree(db)
	if err != nil {
		return 0, err
	}

	return max(size-free, 0), nil
}

// shardSizeAndFree reads one shard's file size and reclaimable (free + pending
// free) bytes through a read transaction.
func shardSizeAndFree(db *bolt.DB) (size, free int64, err error) {
	if viewErr := db.View(func(tx *bolt.Tx) error {
		stats := db.Stats()
		pageSize := int64(db.Info().PageSize)
		free = int64(stats.FreePageN+stats.PendingPageN) * pageSize
		size = tx.Size()

		return nil
	}); viewErr != nil {
		return 0, 0, fmt.Errorf("read shard stats: %w", viewErr)
	}

	return size, free, nil
}

func (e *engine) QuotaBytes() int64 { return e.quotaBytes.Load() }

// SetQuotaBytes changes the live disk-budget ceiling without reopening the
// vault; the eviction sweep and AtCapacity pick it up on their next cycle
// (ADR-0037 D).
func (e *engine) SetQuotaBytes(quotaBytes int64) { e.quotaBytes.Store(quotaBytes) }

func (e *engine) Close() error {
	for _, db := range e.shards {
		if err := wrapCloseError(closeDB(db)); err != nil {
			return err
		}
	}

	return nil
}

func wrapCloseError(err error) error {
	if err != nil {
		return fmt.Errorf("close storage: %w", err)
	}

	return nil
}

// storageAtCapacityError mirrors the boltvault capacity mapping: quota checks
// happen above the engine, so only explicit signals map here.
func storageAtCapacityError(err error) bool {
	return err != nil && err.Error() == vault.ErrAtCapacity.Error()
}

// shardTxn is the lazy multi-shard transaction handed to Engine callers.
type shardTxn struct {
	engine   *engine
	writable bool
	tryLocks bool
	held     []int
	open     []*bolt.Tx
}

func (t *shardTxn) Writable() bool { return t.writable }

// shard returns the open transaction for one shard, opening it on demand. A
// fast-path writer must take the shard's lock first; a locked shard aborts
// the attempt with errShardContended instead of risking a lazy-open deadlock
// against the writer holding it.
func (t *shardTxn) shard(index int) (*bolt.Tx, error) {
	if t.open[index] != nil {
		return t.open[index], nil
	}
	if t.writable && t.tryLocks {
		if !t.engine.shardLocks[index].TryLock() {
			return nil, errShardContended
		}
		t.held = append(t.held, index)
	}
	tx, err := t.engine.shards[index].Begin(t.writable)
	if err != nil {
		return nil, fmt.Errorf("begin shard %d: %w", index, err)
	}
	t.open[index] = tx

	return tx, nil
}

// releaseLocks returns the fast path's shard locks after commit or rollback.
func (t *shardTxn) releaseLocks() {
	for _, index := range t.held {
		t.engine.shardLocks[index].Unlock()
	}
	t.held = nil
}

// commit commits every touched shard; on the first failure the remaining
// open transactions roll back so no shard stays write-locked.
func (t *shardTxn) commit() error {
	var failed error
	for _, tx := range t.open {
		if tx == nil {
			continue
		}
		if failed != nil {
			_ = tx.Rollback()

			continue
		}
		if err := commitTx(tx); err != nil {
			failed = err
			_ = tx.Rollback()
		}
	}

	return failed
}

func (t *shardTxn) rollback() {
	for _, tx := range t.open {
		if tx != nil {
			_ = tx.Rollback()
		}
	}
}

func (t *shardTxn) Bucket(name vault.Name) vault.EngineBucket {
	return &shardBucket{txn: t, name: name}
}

// shardBucket routes each operation to the record's shard.
type shardBucket struct {
	txn  *shardTxn
	name vault.Name
}

// boltBucketFor resolves the bbolt bucket on the record's shard.
func (b *shardBucket) boltBucketFor(key vault.Key) (*bolt.Bucket, error) {
	tx, err := b.txn.shard(b.txn.engine.route(b.name, key))
	if err != nil {
		return nil, err
	}
	bucket := tx.Bucket([]byte(b.name))
	if bucket == nil {
		return nil, fmt.Errorf("bucket %s not provisioned", b.name)
	}

	return bucket, nil
}

func (b *shardBucket) Get(key vault.Key) []byte {
	bucket, err := b.boltBucketFor(key)
	if err != nil {
		return nil
	}
	value, err := decodeValue(bucket.Get(key))
	if err != nil {
		return nil
	}

	return value
}

func (b *shardBucket) Put(key vault.Key, value []byte) error {
	bucket, err := b.boltBucketFor(key)
	if err != nil {
		return err
	}
	if err := bucket.Put(key, encodeValue(value)); err != nil {
		return fmt.Errorf("store: %w", err)
	}

	return nil
}

func (b *shardBucket) Delete(key vault.Key) error {
	bucket, err := b.boltBucketFor(key)
	if err != nil {
		return err
	}
	if err := bucket.Delete(key); err != nil {
		return fmt.Errorf("delete: %w", err)
	}

	return nil
}

// Scan merges the shards' ordered cursors so callers observe one ascending
// key sequence, exactly like a single bbolt file.
func (b *shardBucket) Scan(prefix vault.Key, fn func(vault.Key, []byte) (bool, error)) error {
	cursors, err := b.openCursors(prefix)
	if err != nil {
		return err
	}
	heap.Init(&cursors)
	for cursors.Len() > 0 {
		head, _ := heap.Pop(&cursors).(*shardCursor)
		value, err := decodeValue(head.raw)
		if err != nil {
			return fmt.Errorf("decode stored value: %w", err)
		}
		keep, err := fn(head.key, value)
		if err != nil {
			return err
		}
		if !keep {
			return nil
		}
		key, raw := head.cursor.Next()
		if key != nil && (len(prefix) == 0 || bytes.HasPrefix(key, prefix)) {
			head.key, head.raw = key, raw
			heap.Push(&cursors, head)
		}
	}

	return nil
}

// openCursors positions one cursor per shard at the scan prefix.
func (b *shardBucket) openCursors(prefix vault.Key) (scanHeap, error) {
	cursors := make(scanHeap, 0, len(b.txn.engine.shards))
	for i := range b.txn.engine.shards {
		tx, err := b.txn.shard(i)
		if err != nil {
			return nil, err
		}
		bucket := tx.Bucket([]byte(b.name))
		if bucket == nil {
			continue
		}
		cursor := bucket.Cursor()
		var key, raw []byte
		if len(prefix) == 0 {
			key, raw = cursor.First()
		} else {
			key, raw = cursor.Seek(prefix)
		}
		if key == nil || len(prefix) > 0 && !bytes.HasPrefix(key, prefix) {
			continue
		}
		cursors = append(cursors, &shardCursor{cursor: cursor, key: key, raw: raw})
	}

	return cursors, nil
}

// shardCursor is one shard's position in the merged scan.
type shardCursor struct {
	cursor *bolt.Cursor
	key    []byte
	raw    []byte
}

type scanHeap []*shardCursor

func (h scanHeap) Len() int           { return len(h) }
func (h scanHeap) Less(i, j int) bool { return bytes.Compare(h[i].key, h[j].key) < 0 }
func (h scanHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *scanHeap) Push(
	x any,
) {
	*h = append(*h, x.(*shardCursor))
} //nolint:forcetypeassert // heap of one type.
func (h *scanHeap) Pop() any {
	old := *h
	last := old[len(old)-1]
	*h = old[:len(old)-1]

	return last
}
