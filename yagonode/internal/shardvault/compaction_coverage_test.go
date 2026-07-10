package shardvault

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// churnedEngine fills the store with incompressible records and deletes all but
// a handful, leaving shards whose files are large but mostly free pages — the
// state worthCompacting accepts once the size floor is lowered.
func churnedEngine(t *testing.T) *engine {
	t.Helper()
	e := openTestEngine(t)
	ctx := context.Background()
	if err := e.Update(ctx, func(txn vault.EngineTxn) error {
		b := txn.Bucket(testBucket)
		for i := range 1500 {
			val := []byte(incompressibleValue(uint64(i)))
			if err := b.Put(vault.Key(fmt.Sprintf("k-%05d", i)), val); err != nil {
				return fmt.Errorf("put: %w", err)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("populate: %v", err)
	}
	if err := e.Update(ctx, func(txn vault.EngineTxn) error {
		b := txn.Bucket(testBucket)
		for i := 40; i < 1500; i++ {
			if err := b.Delete(vault.Key(fmt.Sprintf("k-%05d", i))); err != nil {
				return fmt.Errorf("delete: %w", err)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("prune: %v", err)
	}

	return e
}

func firstCompactableShard(t *testing.T, e *engine) int {
	t.Helper()
	for i := range e.shards {
		size, free, err := shardSizeAndFree(e.shards[i])
		if err == nil && worthCompacting(size, free) {
			return i
		}
	}
	t.Fatal("no compactable shard after churn")

	return -1
}

// newBoltFileHandle opens an empty bbolt file and returns the handle with its
// path, for swapShard tests that need a real file at the destination path.
func newBoltFileHandle(t *testing.T) (*bolt.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shard.vlt")
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("open shard file: %v", err)
	}

	return db, path
}

func TestCompactContextCancelled(t *testing.T) {
	e := openTestEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := e.Compact(ctx)
	assertErr(t, err, "compact cancelled")
}

func TestCompactShardMeasureError(t *testing.T) {
	e := openTestEngine(t)
	closeShards(e.shards)
	_, err := e.Compact(context.Background())
	assertErr(t, err, "compact closed shards")
}

func TestCompactShardCompactIntoError(t *testing.T) {
	e := churnedEngine(t)
	defer swapCompactMinBytes(8 << 10)()
	idx := firstCompactableShard(t, e)
	swapOpenBolt(t, func(string, os.FileMode, *bolt.Options) (*bolt.DB, error) {
		return nil, errCov
	})
	_, _, err := e.compactShard(idx)
	assertErr(t, err, "compact into failure")
}

func TestCompactShardSwapError(t *testing.T) {
	e := churnedEngine(t)
	defer swapCompactMinBytes(8 << 10)()
	idx := firstCompactableShard(t, e)
	target := e.shards[idx]
	real := closeDB
	swapCloseDB(t, func(db *bolt.DB) error {
		if db == target {
			return errCov
		}

		return real(db)
	})
	_, _, err := e.compactShard(idx)
	assertErr(t, err, "swap failure")
}

func TestCompactShardReopenMeasureError(t *testing.T) {
	e := churnedEngine(t)
	defer swapCompactMinBytes(8 << 10)()
	idx := firstCompactableShard(t, e)
	real := openBolt
	swapOpenBolt(t, func(p string, m os.FileMode, o *bolt.Options) (*bolt.DB, error) {
		db, err := real(p, m, o)
		if err != nil || strings.HasSuffix(p, compactingSuffix) {
			return db, err
		}
		_ = db.Close()

		return db, nil
	})
	_, _, err := e.compactShard(idx)
	assertErr(t, err, "measure reopened shard")
}

func TestCompactIntoStaleTmpError(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "stale"+compactingSuffix)
	mustNonEmptyDir(t, tmp)
	assertErr(t, compactInto(newSourceShard(t), tmp), "clear stale compaction file")
}

func TestCompactIntoCompactError(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x"+compactingSuffix)
	assertErr(t, compactInto(newClosedShard(t), tmp), "compact closed src")
}

func TestCompactIntoCloseError(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x"+compactingSuffix)
	swapCloseDB(t, func(*bolt.DB) error { return errCov })
	assertErr(t, compactInto(newSourceShard(t), tmp), "close compaction target")
}

func TestSwapShardRenameErrorReopens(t *testing.T) {
	db, path := newBoltFileHandle(t)
	reopened, err := swapShard(db, path+".missing", path)
	assertErr(t, err, "swap rename")
	if reopened == nil {
		t.Fatal("rename failure must reopen the untouched original")
	}
	t.Cleanup(func() { _ = reopened.Close() })
}

func TestSwapShardRenameAndReopenError(t *testing.T) {
	db, path := newBoltFileHandle(t)
	swapOpenBolt(t, func(string, os.FileMode, *bolt.Options) (*bolt.DB, error) {
		return nil, errCov
	})
	reopened, err := swapShard(db, path+".missing", path)
	assertErr(t, err, "swap rename and reopen")
	if reopened != nil {
		t.Fatal("failed reopen must return no handle")
	}
}

func TestSwapShardReopenCompactedError(t *testing.T) {
	db, path := newBoltFileHandle(t)
	tmp := path + compactingSuffix
	if err := os.WriteFile(tmp, []byte("compacted"), 0o600); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	swapOpenBolt(t, func(string, os.FileMode, *bolt.Options) (*bolt.DB, error) {
		return nil, errCov
	})
	reopened, err := swapShard(db, tmp, path)
	assertErr(t, err, "reopen compacted shard")
	if reopened != nil {
		t.Fatal("failed reopen must return no handle")
	}
}
