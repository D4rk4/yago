package main

import (
	"fmt"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func seedPostings(t *testing.T, keys []string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "vault.db")
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(postingBucket))
		if err != nil {
			return fmt.Errorf("create posting bucket: %w", err)
		}
		for _, key := range keys {
			if err := bucket.Put([]byte(key), []byte("posting")); err != nil {
				return fmt.Errorf("put %s: %w", key, err)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("seed postings: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	return path
}

func TestReferenceIndexInvertsPostingKeys(t *testing.T) {
	word := "wwwwwwwwwwww"
	url := "uuuuuuuuuuuu"
	path := seedPostings(t, []string{word + url})

	migrateDB(t, path)

	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.View(func(tx *bolt.Tx) error {
		words := tx.Bucket([]byte(wordsByURLBucket))
		if words.Get([]byte(url+word)) == nil {
			t.Errorf("reverse key %q missing", url+word)
		}
		assertLength(t, tx, wordsByURLBucket, 1)

		return nil
	}); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestReferenceIndexSkipsMalformedKeys(t *testing.T) {
	path := seedPostings(t, []string{"short", "wwwwwwwwwwwwuuuuuuuuuuuu"})

	migrateDB(t, path)

	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.View(func(tx *bolt.Tx) error {
		assertLength(t, tx, wordsByURLBucket, 1)

		return nil
	}); err != nil {
		t.Fatalf("verify: %v", err)
	}
}
