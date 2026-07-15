package shardvault

import (
	"fmt"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestShardBucketReadsLastKey(t *testing.T) {
	shards := openTestEngine(t)
	writeRecords(t, shards, 300)
	var key vault.Key
	if err := shards.View(t.Context(), func(txn vault.EngineTxn) error {
		read, err := txn.Bucket(testBucket).(*shardBucket).LastKey()
		key = read

		return err
	}); err != nil {
		t.Fatal(err)
	}
	if string(key) != "key-00299" {
		t.Fatalf("last key = %q", key)
	}
}

func TestShardBucketLastKeyIgnoresDeletedStaleSplitCopy(t *testing.T) {
	shards := openTestEngine(t)
	writeRecords(t, shards, 300)
	candidates := storeLastKeyCandidates(t, shards, 64)
	if moved, err := shards.SplitStep(t.Context()); err != nil || !moved {
		t.Fatalf("split = %v, %v", moved, err)
	}
	staleKey := selectMovedLastKeyCandidate(t, shards, candidates)
	deleteLastKeyCandidates(t, shards, candidates)
	restoreLastKeyStaleCopy(t, shards, staleKey)
	var key vault.Key
	if err := shards.View(t.Context(), func(txn vault.EngineTxn) error {
		read, err := txn.Bucket(testBucket).(*shardBucket).LastKey()
		key = read

		return err
	}); err != nil {
		t.Fatal(err)
	}
	if string(key) != "key-00299" {
		t.Fatalf("last key with stale %q = %q", staleKey, key)
	}
}

func storeLastKeyCandidates(t *testing.T, shards *engine, total int) []string {
	t.Helper()
	candidates := make([]string, 0, total)
	if err := shards.Update(t.Context(), func(txn vault.EngineTxn) error {
		bucket := txn.Bucket(testBucket)
		for index := range total {
			key := fmt.Sprintf("zz-candidate-%05d", index)
			candidates = append(candidates, key)
			if err := bucket.Put(vault.Key(key), []byte("value")); err != nil {
				return fmt.Errorf("put %s: %w", key, err)
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	return candidates
}

func selectMovedLastKeyCandidate(
	t *testing.T,
	shards *engine,
	candidates []string,
) string {
	t.Helper()
	for index := len(candidates) - 1; index >= 0; index-- {
		if shards.route(testBucket, vault.Key(candidates[index])) == 8 {
			return candidates[index]
		}
	}
	t.Fatal("no moved last-key candidate")

	return ""
}

func deleteLastKeyCandidates(t *testing.T, shards *engine, candidates []string) {
	t.Helper()
	if err := shards.Update(t.Context(), func(txn vault.EngineTxn) error {
		bucket := txn.Bucket(testBucket)
		for _, key := range candidates {
			if err := bucket.Delete(vault.Key(key)); err != nil {
				return fmt.Errorf("delete %s: %w", key, err)
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func restoreLastKeyStaleCopy(t *testing.T, shards *engine, key string) {
	t.Helper()
	if err := shards.shards[0].Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(testBucket))
		if bucket == nil {
			return fmt.Errorf("bucket %s missing", testBucket)
		}

		return bucket.Put([]byte(key), encodeValue([]byte("stale")))
	}); err != nil {
		t.Fatal(err)
	}
}

func TestShardBucketLastKeyHandlesMissingBucketAndShardError(t *testing.T) {
	db, err := bolt.Open(filepath.Join(t.TempDir(), "empty.db"), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	shards := &engine{shards: []*bolt.DB{db}}
	txn := &shardTxn{engine: shards, open: make([]*bolt.Tx, 1)}
	key, err := (&shardBucket{txn: txn, name: "missing"}).LastKey()
	txn.rollback()
	if err != nil || key != nil {
		t.Fatalf("missing bucket last key = %q, %v", key, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	txn = &shardTxn{engine: shards, open: make([]*bolt.Tx, 1)}
	if _, err := (&shardBucket{txn: txn, name: "missing"}).LastKey(); err == nil {
		t.Fatal("closed shard last key succeeded")
	}
}
