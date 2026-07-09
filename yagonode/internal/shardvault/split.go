package shardvault

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cespare/xxhash/v2"
	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const splittingSuffix = ".splitting"

// splitTxMaxBytes bounds one write transaction of the split copy and cleanup so
// moving a full shard does not build a single giant transaction (mirrors
// compactTxMaxSize). It is a var so a test can lower it to exercise batching.
var splitTxMaxBytes = int64(16) << 20

// SplitStep grows the shard pool by one under linear hashing (ADR-0037): the
// split-pointer shard is halved — its records that now hash into the new shard
// move there — and the layout pointer advances. It runs under the exclusive
// gate, so no transaction is in flight while the layout changes and the copy
// sees a stable source. A crash is safe by the copy -> manifest-flip -> cleanup
// ordering: before the flip the new shard is unrouted and the source is intact
// (retry), after it the moved records live in the new shard and the stale copies
// left in the source are inert (a bounded space leak, reclaimed by cleanup or a
// later compaction). It returns whether a split happened (false at the cap).
func (e *engine) SplitStep(ctx context.Context) (bool, error) {
	e.globalGate.Lock()
	defer e.globalGate.Unlock()

	return e.splitLocked(ctx)
}

func (e *engine) splitLocked(ctx context.Context) (bool, error) {
	oldLevel, oldSplit := e.level, e.split
	n := (1 << oldLevel) + oldSplit
	if n >= maxShards {
		return false, nil
	}
	opened, err := e.buildSplitShard(ctx, e.shards[oldSplit], oldLevel, n)
	if err != nil {
		return false, err
	}
	// Flip the manifest — the commit point. Past here the new shard is routable
	// and the split is durable; a failure before it leaves the source serving.
	newLevel, newSplit := advanceSplit(oldLevel, oldSplit)
	if err := writeManifest(e.dir, layoutManifest{Level: newLevel, Split: newSplit}); err != nil {
		_ = closeDB(opened)

		return false, fmt.Errorf("commit split: %w", err)
	}
	e.shards = append(e.shards, opened)
	e.level, e.split = newLevel, newSplit
	// Reclaim the now-misrouted copies from the source. A failure here is
	// non-fatal: the records are already served from the new shard, so the leftover
	// is dead space, not lost data.
	if err := deleteMovedRecords(ctx, e.shards[oldSplit], oldLevel, n); err != nil {
		return true, fmt.Errorf("split cleanup: %w", err)
	}

	return true, nil
}

// buildSplitShard writes a fresh shard file holding the source records that move
// to index n, then installs it at its shard path and returns an open handle. It
// does not touch the source or the manifest, so any failure here leaves the
// store exactly as it was.
func (e *engine) buildSplitShard(
	ctx context.Context,
	src *bolt.DB,
	level, n int,
) (*bolt.DB, error) {
	path := shardPath(e.dir, n)
	tmp := path + splittingSuffix
	if err := os.Remove(tmp); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("clear stale split file: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create shard directory: %w", err)
	}
	dst, err := openBolt(tmp, 0o600, openTimeoutOptions())
	if err != nil {
		return nil, fmt.Errorf("open split target: %w", err)
	}
	if err := copyMovedRecords(ctx, src, dst, level, n); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmp)

		return nil, err
	}
	if err := closeDB(dst); err != nil {
		_ = os.Remove(tmp)

		return nil, fmt.Errorf("close split target: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)

		return nil, fmt.Errorf("install split shard: %w", err)
	}
	opened, err := openBolt(path, 0o600, openTimeoutOptions())
	if err != nil {
		return nil, fmt.Errorf("open split shard: %w", err)
	}

	return opened, nil
}

// advanceSplit steps the linear-hashing pointer: the split pointer walks the
// current level's buckets, and when it completes a round the level doubles and
// the pointer resets.
func advanceSplit(level, split int) (int, int) {
	if split+1 >= 1<<level {
		return level + 1, 0
	}

	return level, split + 1
}

// movesTo reports whether a record hashing under the pre-split level relocates
// to the new shard n — its wide (level+1) index selects n rather than the
// split-pointer shard.
func movesTo(name vault.Name, key vault.Key, level, n int) bool {
	hash := xxhash.New()
	_, _ = hash.WriteString(string(name))
	_, _ = hash.Write(key)
	wide := hash.Sum64() & (uint64(1)<<(level+1) - 1)

	return int(wide) == n //nolint:gosec // wide < 2^(level+1) ≤ 2·maxShards.
}

// copyMovedRecords copies every bucket of src into dst, carrying the records
// that move to shard n. Every bucket is created in dst (even with no moving
// record) so a later write routing there finds it provisioned. The destination
// commits in bounded batches so a large shard does not build one transaction.
func copyMovedRecords(ctx context.Context, src, dst *bolt.DB, level, n int) error {
	copier, err := newSplitCopier(dst)
	if err != nil {
		return err
	}
	err = src.View(func(tx *bolt.Tx) error {
		return tx.ForEach(func(name []byte, sb *bolt.Bucket) error {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return fmt.Errorf("context: %w", ctxErr)
			}
			if startErr := copier.startBucket(name); startErr != nil {
				return startErr
			}

			return sb.ForEach(func(k, v []byte) error {
				if !movesTo(vault.Name(name), k, level, n) {
					return nil
				}

				return copier.put(k, v)
			})
		})
	})
	if err != nil {
		copier.abort()

		return fmt.Errorf("copy split records: %w", err)
	}

	return copier.commit()
}

// splitCopier streams records into the new shard, cycling its write transaction
// every splitTxMaxBytes and re-provisioning the current bucket in each fresh
// transaction.
type splitCopier struct {
	dst     *bolt.DB
	tx      *bolt.Tx
	bucket  *bolt.Bucket
	name    []byte
	pending int64
}

func newSplitCopier(dst *bolt.DB) (*splitCopier, error) {
	tx, err := dst.Begin(true)
	if err != nil {
		return nil, fmt.Errorf("begin split write: %w", err)
	}

	return &splitCopier{dst: dst, tx: tx}, nil
}

func (c *splitCopier) startBucket(name []byte) error {
	bucket, err := c.tx.CreateBucketIfNotExists(name)
	if err != nil {
		return fmt.Errorf("create split bucket: %w", err)
	}
	c.bucket = bucket
	c.name = append(c.name[:0], name...)

	return nil
}

func (c *splitCopier) put(key, value []byte) error {
	if err := c.bucket.Put(key, value); err != nil {
		return fmt.Errorf("split put: %w", err)
	}
	c.pending += int64(len(key) + len(value))
	if c.pending < splitTxMaxBytes {
		return nil
	}
	name := append([]byte{}, c.name...)
	if err := commitTx(c.tx); err != nil {
		return fmt.Errorf("commit split batch: %w", err)
	}
	c.pending = 0
	tx, err := c.dst.Begin(true)
	if err != nil {
		return fmt.Errorf("begin split write: %w", err)
	}
	c.tx = tx

	return c.startBucket(name)
}

func (c *splitCopier) commit() error {
	if err := commitTx(c.tx); err != nil {
		return fmt.Errorf("commit split: %w", err)
	}
	c.tx = nil

	return nil
}

func (c *splitCopier) abort() {
	if c.tx != nil {
		_ = c.tx.Rollback()
	}
}

// deleteMovedRecords removes, from the source shard, the records that the split
// relocated to shard n, per bucket and in bounded batches.
func deleteMovedRecords(ctx context.Context, src *bolt.DB, level, n int) error {
	names, err := bucketNames(src)
	if err != nil {
		return err
	}
	for _, name := range names {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("context: %w", ctxErr)
		}
		if err := deleteMovedFromBucket(src, name, level, n); err != nil {
			return err
		}
	}

	return nil
}

// deleteMovedFromBucket deletes one bucket's moved records, cycling the write
// transaction every splitTxMaxBytes and resuming past the last key each round.
func deleteMovedFromBucket(src *bolt.DB, name []byte, level, n int) error {
	var seek []byte
	for {
		next, err := deleteMovedBatch(src, name, level, n, seek)
		if err != nil {
			return err
		}
		if next == nil {
			return nil
		}
		seek = next
	}
}

// deleteMovedBatch scans one bucket from seek, collecting the moved keys until it
// has covered roughly splitTxMaxBytes, then deletes them. Collecting before
// deleting keeps the read cursor and the deletes from interleaving. It returns
// the resume key, or nil when the bucket is exhausted.
func deleteMovedBatch(src *bolt.DB, name []byte, level, n int, seek []byte) ([]byte, error) {
	var resume []byte
	err := src.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(name)
		if bucket == nil {
			return nil
		}
		cursor := bucket.Cursor()
		var batch [][]byte
		var span int64
		for k, v := seekStart(cursor, seek); k != nil; k, v = cursor.Next() {
			if !movesTo(vault.Name(name), k, level, n) {
				continue
			}
			batch = append(batch, append([]byte{}, k...))
			span += int64(len(k) + len(v))
			if span >= splitTxMaxBytes {
				resume = append([]byte{}, k...)

				break
			}
		}

		return deleteKeys(bucket, batch)
	})
	if err != nil {
		return nil, fmt.Errorf("delete moved batch: %w", err)
	}

	return resume, nil
}

func deleteKeys(bucket *bolt.Bucket, keys [][]byte) error {
	for _, key := range keys {
		if err := bucket.Delete(key); err != nil {
			return fmt.Errorf("split cleanup delete: %w", err)
		}
	}

	return nil
}

func seekStart(cursor *bolt.Cursor, seek []byte) ([]byte, []byte) {
	if seek == nil {
		return cursor.First()
	}

	return cursor.Seek(seek)
}

// bucketNames lists the buckets in a shard so the cleanup can visit each in its
// own transaction rather than holding one open across the whole shard.
func bucketNames(db *bolt.DB) ([][]byte, error) {
	var names [][]byte
	err := db.View(func(tx *bolt.Tx) error {
		return tx.ForEach(func(name []byte, _ *bolt.Bucket) error {
			names = append(names, append([]byte{}, name...))

			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("list buckets: %w", err)
	}

	return names, nil
}
