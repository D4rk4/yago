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
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/cespare/xxhash/v2"
	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	shardBytesTarget = int64(7) << 30
	minShards        = 8
	maxShards        = 1024
)

var newVault = vault.New

// commitTx and closeDB are seams so tests can exercise commit and close
// failures.
var (
	commitTx = (*bolt.Tx).Commit
	closeDB  = (*bolt.DB).Close
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
	manifest, err := loadOrCreateManifest(dir, shardCountForQuota(quotaBytes))
	if err != nil {
		return nil, err
	}
	shards := make([]*bolt.DB, manifest.Shards)
	for i := range shards {
		path := shardPath(dir, i)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			closeShards(shards)

			return nil, fmt.Errorf("create shard directory: %w", err)
		}
		db, err := bolt.Open(path, 0o600, nil)
		if err != nil {
			closeShards(shards)

			return nil, fmt.Errorf("open shard %d: %w", i, err)
		}
		shards[i] = db
	}

	return &engine{shards: shards, dir: dir, quotaBytes: quotaBytes}, nil
}

// shardCountForQuota sizes the shard pool so a full store keeps files well
// under their cap: one shard per ~7 GB, a power of two, bounded.
func shardCountForQuota(quotaBytes int64) int {
	count := minShards
	for int64(count)*shardBytesTarget < quotaBytes && count < maxShards {
		count *= 2
	}

	return count
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
	shards     []*bolt.DB
	dir        string
	quotaBytes int64
	// writeGate serializes writers: bbolt allows one write transaction per
	// file, and lazily opening several shards from concurrent updates would
	// deadlock on each other's held shards.
	writeGate sync.Mutex
}

// route picks the shard for one record.
func (e *engine) route(bucket vault.Name, key vault.Key) int {
	hash := xxhash.New()
	_, _ = hash.WriteString(string(bucket))
	_, _ = hash.Write(key)

	return int(hash.Sum64() % uint64(len(e.shards))) //nolint:gosec // bounded by len(shards).
}

func (e *engine) Provision(name vault.Name) error {
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
// relaxed atomicity ADR-0025 documents.
func (e *engine) Update(_ context.Context, fn func(vault.EngineTxn) error) error {
	e.writeGate.Lock()
	defer e.writeGate.Unlock()
	txn := &shardTxn{engine: e, writable: true, open: make([]*bolt.Tx, len(e.shards))}
	if err := fn(txn); err != nil {
		txn.rollback()
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
	txn := &shardTxn{engine: e, open: make([]*bolt.Tx, len(e.shards))}
	defer txn.rollback()
	if err := fn(txn); err != nil {
		return err
	}

	return nil
}

func (e *engine) UsedBytes(_ context.Context) (int64, error) {
	var total int64
	for i := range e.shards {
		info, err := os.Stat(shardPath(e.dir, i))
		if err != nil {
			continue
		}
		total += info.Size()
	}

	return total, nil
}

func (e *engine) QuotaBytes() int64 { return e.quotaBytes }

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
	open     []*bolt.Tx
}

func (t *shardTxn) Writable() bool { return t.writable }

// shard returns the open transaction for one shard, opening it on demand.
func (t *shardTxn) shard(index int) (*bolt.Tx, error) {
	if t.open[index] != nil {
		return t.open[index], nil
	}
	tx, err := t.engine.shards[index].Begin(t.writable)
	if err != nil {
		return nil, fmt.Errorf("begin shard %d: %w", index, err)
	}
	t.open[index] = tx

	return tx, nil
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
