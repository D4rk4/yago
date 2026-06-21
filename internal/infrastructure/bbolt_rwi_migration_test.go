package infrastructure

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestMigrateRWIPostingsRewritesLegacyValues(t *testing.T) {
	ctx := context.Background()
	store := openTestStorage(t, filepath.Join(t.TempDir(), "node.db"), 0)
	defer closeTestStorage(t, store)

	word := hashForStorageTest("word")
	entry := rwiPostingForStorageTest(word, "url-a", 3)
	seedLegacyPosting(t, store, entry)

	report, err := store.MigrateRWIPostings(ctx)
	if err != nil {
		t.Fatalf("MigrateRWIPostings: %v", err)
	}
	if report.Scanned != 1 || report.Rewritten != 1 {
		t.Fatalf("report = %+v, want scanned 1 rewritten 1", report)
	}

	urlHash, err := entry.URLHash()
	if err != nil {
		t.Fatalf("URLHash: %v", err)
	}
	if value := storedPosting(t, store, rwiPostingKey(word, urlHash)); !isBinaryPosting(value) {
		t.Fatalf("value not migrated to binary: %q", value)
	}

	result, err := store.SearchPostings(ctx, ports.PostingSearchQuery{
		WordHashes: []yacymodel.Hash{word},
	})
	if err != nil {
		t.Fatalf("SearchPostings: %v", err)
	}
	postings := result.Postings[word]
	if len(postings) != 1 {
		t.Fatalf("postings = %d, want 1", len(postings))
	}
	if got := postings[0].Properties[yacymodel.ColWordDistance]; got != decimalForTest(3) {
		t.Fatalf("word distance = %q, want %q", got, decimalForTest(3))
	}

	again, err := store.MigrateRWIPostings(ctx)
	if err != nil {
		t.Fatalf("second MigrateRWIPostings: %v", err)
	}
	if again.Rewritten != 0 {
		t.Fatalf("second run rewritten = %d, want 0", again.Rewritten)
	}
}

func TestMigrateRWIPostingsCrossesBatchBoundary(t *testing.T) {
	ctx := context.Background()
	store := openTestStorage(t, filepath.Join(t.TempDir(), "node.db"), 0)
	defer closeTestStorage(t, store)

	word := hashForStorageTest("word")
	const total = rwiMigrationBatch + 5
	for i := range total {
		seedLegacyPosting(t, store, rwiPostingForStorageTest(word, fmt.Sprintf("url-%d", i), 1))
	}

	report, err := store.MigrateRWIPostings(ctx)
	if err != nil {
		t.Fatalf("MigrateRWIPostings: %v", err)
	}
	if report.Scanned != total || report.Rewritten != total {
		t.Fatalf("report = %+v, want scanned and rewritten %d", report, total)
	}

	again, err := store.MigrateRWIPostings(ctx)
	if err != nil {
		t.Fatalf("second MigrateRWIPostings: %v", err)
	}
	if again.Scanned != total || again.Rewritten != 0 {
		t.Fatalf("second report = %+v, want scanned %d rewritten 0", again, total)
	}
}

func TestMigrateRWIPostingsHonorsCancellation(t *testing.T) {
	store := openTestStorage(t, filepath.Join(t.TempDir(), "node.db"), 0)
	defer closeTestStorage(t, store)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.MigrateRWIPostings(ctx); err == nil {
		t.Fatal("expected cancellation error")
	}
}

func seedLegacyPosting(t *testing.T, store *BboltStorage, entry yacymodel.RWIPosting) {
	t.Helper()

	urlHash, err := entry.URLHash()
	if err != nil {
		t.Fatalf("URLHash: %v", err)
	}
	key := rwiPostingKey(entry.WordHash, urlHash)
	err = store.update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketRWI).Put(key, []byte(entry.String()))
	})
	if err != nil {
		t.Fatalf("seed legacy posting: %v", err)
	}
}

func storedPosting(t *testing.T, store *BboltStorage, key []byte) []byte {
	t.Helper()

	var value []byte
	err := store.view(func(tx *bolt.Tx) error {
		value = append([]byte(nil), tx.Bucket(bucketRWI).Get(key)...)
		return nil
	})
	if err != nil {
		t.Fatalf("read posting: %v", err)
	}

	return value
}
