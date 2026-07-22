package shardvault

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestShardBucketInspectionReportsClosedShard(t *testing.T) {
	db, err := bolt.Open(filepath.Join(t.TempDir(), "closed.db"), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	engine := &engine{shards: []*bolt.DB{db}}
	txn := &shardTxn{engine: engine, open: make([]*bolt.Tx, 1)}
	bucket := &shardBucket{txn: txn, name: "documents"}
	if _, err := bucket.ReadKeyPageAfter(nil, 1); err == nil {
		t.Fatal("closed shard key page succeeded")
	}
	if _, _, err := bucket.ValueSize(vault.Key("a")); err == nil {
		t.Fatal("closed shard value size succeeded")
	}
	txn.rollback()
}

func TestShardBucketValueSizeRejectsCorruptStoredValue(t *testing.T) {
	db, err := bolt.Open(filepath.Join(t.TempDir(), "corrupt.db"), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucket([]byte("documents"))
		if err != nil {
			return fmt.Errorf("create documents bucket: %w", err)
		}
		if err := bucket.Put([]byte("a"), []byte{0xff}); err != nil {
			return fmt.Errorf("put corrupt value: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	engine := &engine{shards: []*bolt.DB{db}}
	txn := &shardTxn{engine: engine, open: make([]*bolt.Tx, 1)}
	_, found, err := (&shardBucket{txn: txn, name: "documents"}).ValueSize(vault.Key("a"))
	txn.rollback()
	if !errors.Is(err, vault.ErrCorruptValue) || !found {
		t.Fatalf("corrupt value size = found %t, error %v", found, err)
	}
}
