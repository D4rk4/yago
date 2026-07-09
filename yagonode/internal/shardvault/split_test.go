package shardvault

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const testBucket = vault.Name("docs")

func openTestEngine(t *testing.T) *engine {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "vault")
	e, err := openEngine(dir, 1<<20) // level 3, split 0 → 8 shards
	if err != nil {
		t.Fatalf("openEngine: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })
	if err := e.Provision(testBucket); err != nil {
		t.Fatalf("provision: %v", err)
	}

	return e
}

func writeRecords(t *testing.T, e *engine, n int) map[string]string {
	t.Helper()
	want := make(map[string]string, n)
	err := e.Update(context.Background(), func(txn vault.EngineTxn) error {
		b := txn.Bucket(testBucket)
		for i := range n {
			key := fmt.Sprintf("key-%05d", i)
			val := fmt.Sprintf("value-%05d", i)
			want[key] = val
			if err := b.Put(vault.Key(key), []byte(val)); err != nil {
				return fmt.Errorf("put %s: %w", key, err)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("write records: %v", err)
	}

	return want
}

func readAllRecords(t *testing.T, e *engine) map[string]string {
	t.Helper()
	got := make(map[string]string)
	err := e.View(context.Background(), func(txn vault.EngineTxn) error {
		return txn.Bucket(testBucket).Scan(nil, func(k vault.Key, v []byte) (bool, error) {
			got[string(k)] = string(v)

			return true, nil
		})
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	return got
}

func assertSameRecords(t *testing.T, want, got map[string]string) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("record count = %d, want %d", len(got), len(want))
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("key %s = %q, want %q", k, got[k], v)
		}
	}
}

// TestSplitStepPreservesRecords: one split advances the layout, keeps every
// record readable at its correct value, and genuinely relocates some keys to the
// new shard.
func TestSplitStepPreservesRecords(t *testing.T) {
	e := openTestEngine(t)
	want := writeRecords(t, e, 3000)

	moved, err := e.SplitStep(context.Background())
	if err != nil || !moved {
		t.Fatalf("split = %v, %v", moved, err)
	}
	if e.level != 3 || e.split != 1 || len(e.shards) != 9 {
		t.Fatalf(
			"after split: level %d split %d shards %d, want 3/1/9",
			e.level, e.split, len(e.shards),
		)
	}
	assertSameRecords(t, want, readAllRecords(t, e))

	moved8 := 0
	for k := range want {
		if e.route(testBucket, vault.Key(k)) == 8 {
			moved8++
		}
	}
	if moved8 == 0 {
		t.Fatal("split moved no key to the new shard")
	}
}

// TestSplitStepRoundRollsLevel: a full round of splits (2^level) doubles the
// level, resets the pointer, and keeps every record.
func TestSplitStepRoundRollsLevel(t *testing.T) {
	e := openTestEngine(t)
	want := writeRecords(t, e, 2000)

	for range 8 {
		if _, err := e.SplitStep(context.Background()); err != nil {
			t.Fatalf("split: %v", err)
		}
	}
	if e.level != 4 || e.split != 0 || len(e.shards) != 16 {
		t.Fatalf(
			"after round: level %d split %d shards %d, want 4/0/16",
			e.level, e.split, len(e.shards),
		)
	}
	assertSameRecords(t, want, readAllRecords(t, e))
}

// TestSplitReopenPersists: a split pool's state survives a close/open cycle
// through the version-2 manifest, and every record is still readable.
func TestSplitReopenPersists(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	e, err := openEngine(dir, 1<<20)
	if err != nil {
		t.Fatalf("openEngine: %v", err)
	}
	if err := e.Provision(testBucket); err != nil {
		t.Fatalf("provision: %v", err)
	}
	want := writeRecords(t, e, 2500)
	for range 3 {
		if _, err := e.SplitStep(context.Background()); err != nil {
			t.Fatalf("split: %v", err)
		}
	}
	if err := e.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := openEngine(dir, 1<<20)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if reopened.level != 3 || reopened.split != 3 || len(reopened.shards) != 11 {
		t.Fatalf(
			"reopened: level %d split %d shards %d, want 3/3/11",
			reopened.level, reopened.split, len(reopened.shards),
		)
	}
	assertSameRecords(t, want, readAllRecords(t, reopened))
}

// TestSplitCleanupRemovesMovedCopies: after a split, a key that now routes to the
// new shard is gone from the source shard's file — no dead copy is left behind.
func TestSplitCleanupRemovesMovedCopies(t *testing.T) {
	e := openTestEngine(t)
	want := writeRecords(t, e, 3000)
	if _, err := e.SplitStep(context.Background()); err != nil {
		t.Fatalf("split: %v", err)
	}

	checked := 0
	for k := range want {
		if e.route(testBucket, vault.Key(k)) != 8 {
			continue
		}
		checked++
		if raw := rawShardGet(t, e.shards[0], k); raw != nil {
			t.Fatalf("moved key %s still present in source shard 0", k)
		}
	}
	if checked == 0 {
		t.Fatal("no moved keys to verify")
	}
}

// TestSplitBatchesLargeShard forces many copy and cleanup batches by shrinking
// the per-transaction budget, exercising the transaction cycling and the delete
// resume path; every record must survive.
func TestSplitBatchesLargeShard(t *testing.T) {
	saved := splitTxMaxBytes
	splitTxMaxBytes = 1 << 10
	t.Cleanup(func() { splitTxMaxBytes = saved })

	e := openTestEngine(t)
	want := writeRecords(t, e, 6000)
	if _, err := e.SplitStep(context.Background()); err != nil {
		t.Fatalf("split: %v", err)
	}
	assertSameRecords(t, want, readAllRecords(t, e))
}

func rawShardGet(t *testing.T, db *bolt.DB, key string) []byte {
	t.Helper()
	var out []byte
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(testBucket))
		if b == nil {
			return nil
		}
		out = b.Get([]byte(key))

		return nil
	})
	if err != nil {
		t.Fatalf("raw get: %v", err)
	}

	return out
}
