package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func seedURLMetadata(t *testing.T, entries map[string][]byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "vault.db")
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		bucket, createErr := tx.CreateBucketIfNotExists([]byte(urlMetadataBucket))
		if createErr != nil {
			return fmt.Errorf("create bucket: %w", createErr)
		}
		for key, value := range entries {
			if putErr := bucket.Put([]byte(key), value); putErr != nil {
				return fmt.Errorf("put %s: %w", key, putErr)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("seed url metadata: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	return path
}

func TestStalenessOrderRanksStalestFirst(t *testing.T) {
	path := seedURLMetadata(t, map[string][]byte{
		"stale":  encodedRow(t, "stale", "20200101"),
		"fresh":  encodedRow(t, "fresh", "20260101"),
		"middle": encodedRow(t, "middle", "20230101"),
	})

	migrateDB(t, path)

	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.View(func(tx *bolt.Tx) error {
		first, _ := tx.Bucket([]byte(stalenessOrderBucket)).Cursor().First()
		if !bytes.HasSuffix(first, []byte("stale")) {
			t.Errorf("stalest order key = %q, want suffix \"stale\"", first)
		}
		assertLength(t, tx, stalenessOrderBucket, 3)
		assertLength(t, tx, stalenessFreshnessBucket, 3)

		return nil
	}); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestStalenessMigrationReapsCorruptRows(t *testing.T) {
	path := seedURLMetadata(t, map[string][]byte{
		"good":    encodedRow(t, "good", "20200101"),
		"corrupt": {9},
	})

	migrateDB(t, path)

	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.View(func(tx *bolt.Tx) error {
		urls := tx.Bucket([]byte(urlMetadataBucket))
		if urls.Get([]byte("corrupt")) != nil {
			t.Error("corrupt row should be deleted")
		}
		if urls.Get([]byte("good")) == nil {
			t.Error("good row should be kept")
		}
		assertLength(t, tx, urlMetadataBucket, 1)
		assertLength(t, tx, stalenessOrderBucket, 1)

		return nil
	}); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func migrateDB(t *testing.T, path string) {
	t.Helper()

	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
}

func assertLength(t *testing.T, tx *bolt.Tx, bucket string, want uint64) {
	t.Helper()

	got := binary.BigEndian.Uint64(tx.Bucket([]byte(lengthBucket)).Get([]byte(bucket)))
	if got != want {
		t.Errorf("length %s = %d, want %d", bucket, got, want)
	}
}
