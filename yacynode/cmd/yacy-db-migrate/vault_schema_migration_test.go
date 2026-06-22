package main

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func seedLegacyDB(t *testing.T) (string, map[string]uint64) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}

	legacy := map[string]map[string][]byte{
		"rwi":             {"w1u1": {1}, "w1u2": {2}, "w2u1": {3}},
		"referenced_urls": {"u1": {1}, "u2": {1}},
		"urls":            {"u1": {9}},
		"counts":          {"rwi": le(7), "referenced_urls": le(5), "urls": le(3)},
	}

	if err := db.Update(func(tx *bolt.Tx) error {
		for name, entries := range legacy {
			bucket, createErr := tx.CreateBucketIfNotExists([]byte(name))
			if createErr != nil {
				return fmt.Errorf("create %s: %w", name, createErr)
			}
			for key, value := range entries {
				if putErr := bucket.Put([]byte(key), value); putErr != nil {
					return fmt.Errorf("put %s/%s: %w", name, key, putErr)
				}
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("seed legacy db: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	return path, map[string]uint64{"rwi": 3, "rwi_refs": 2, "urlmeta": 1}
}

func le(n uint64) []byte {
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], n)

	return raw[:]
}

func migrateLegacyDB(t *testing.T) (string, map[string]uint64) {
	t.Helper()

	path, wantLengths := seedLegacyDB(t)

	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	migrated, err := migrate(db)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !migrated {
		t.Fatal("expected first migration to report migrated=true")
	}

	return path, wantLengths
}

func assertBucketsRenamed(t *testing.T, tx *bolt.Tx) {
	t.Helper()

	for _, gone := range []string{"referenced_urls", "urls", "counts"} {
		if tx.Bucket([]byte(gone)) != nil {
			t.Errorf("bucket %s should be removed", gone)
		}
	}
	if tx.Bucket([]byte("rwi_refs")).Get([]byte("u1")) == nil {
		t.Error("renamed rwi_refs missing copied key u1")
	}
	if tx.Bucket([]byte("urlmeta")).Get([]byte("u1")) == nil {
		t.Error("renamed urlmeta missing copied key u1")
	}
	if tx.Bucket([]byte("rwi")).Get([]byte("w1u1")) == nil {
		t.Error("rwi bucket should be left untouched")
	}
}

func assertLengths(t *testing.T, tx *bolt.Tx, want map[string]uint64) {
	t.Helper()

	lengths := tx.Bucket([]byte(lengthBucket))
	for name, wantLength := range want {
		if got := binary.BigEndian.Uint64(lengths.Get([]byte(name))); got != wantLength {
			t.Errorf("length %s = %d, want %d", name, got, wantLength)
		}
	}
	if string(lengths.Get([]byte(schemaKey))) != schemaVault {
		t.Error("schema marker not written")
	}
}

func TestMigrateRelocatesLegacySchema(t *testing.T) {
	path, wantLengths := migrateLegacyDB(t)

	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.View(func(tx *bolt.Tx) error {
		assertBucketsRenamed(t, tx)
		assertLengths(t, tx, wantLengths)

		return nil
	}); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestMigrateRecomputesDriftedCounts(t *testing.T) {
	path, wantLengths := migrateLegacyDB(t)

	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.View(func(tx *bolt.Tx) error {
		got := binary.BigEndian.Uint64(tx.Bucket([]byte(lengthBucket)).Get([]byte("rwi")))
		if got != wantLengths["rwi"] {
			t.Errorf(
				"rwi length = %d, want recomputed %d (legacy stored 7)",
				got,
				wantLengths["rwi"],
			)
		}

		return nil
	}); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	path, _ := seedLegacyDB(t)

	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := migrate(db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}

	migrated, err := migrate(db)
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if migrated {
		t.Fatal("expected second migration to report migrated=false")
	}
}
