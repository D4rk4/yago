package infrastructure

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestMigrateURLMetadataRewritesLegacyValues(t *testing.T) {
	ctx := context.Background()
	store := openTestStorage(t, filepath.Join(t.TempDir(), "node.db"), 0)
	defer closeTestStorage(t, store)

	row := urlRowForStorageTest("url-a")
	seedLegacyURLMetadata(t, store, row)

	report, err := store.MigrateURLMetadata(ctx)
	if err != nil {
		t.Fatalf("MigrateURLMetadata: %v", err)
	}
	if report.Scanned != 1 || report.Rewritten != 1 {
		t.Fatalf("report = %+v, want scanned 1 rewritten 1", report)
	}

	hash, err := row.URLHash()
	if err != nil {
		t.Fatalf("URLHash: %v", err)
	}
	if value := storedURLMetadata(t, store, []byte(hash)); !isCompressedURLMetadata(value) {
		t.Fatalf("value not migrated to compressed form: %q", value)
	}

	rows, err := store.RowsByHash(ctx, []yacymodel.Hash{hash})
	if err != nil {
		t.Fatalf("RowsByHash: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}

	again, err := store.MigrateURLMetadata(ctx)
	if err != nil {
		t.Fatalf("second MigrateURLMetadata: %v", err)
	}
	if again.Rewritten != 0 {
		t.Fatalf("second run rewritten = %d, want 0", again.Rewritten)
	}
}

func TestMigrateURLMetadataCrossesBatchBoundary(t *testing.T) {
	ctx := context.Background()
	store := openTestStorage(t, filepath.Join(t.TempDir(), "node.db"), 0)
	defer closeTestStorage(t, store)

	const total = urlMigrationBatch + 5
	for i := range total {
		seedLegacyURLMetadata(t, store, urlRowForStorageTest(fmt.Sprintf("url-%d", i)))
	}

	report, err := store.MigrateURLMetadata(ctx)
	if err != nil {
		t.Fatalf("MigrateURLMetadata: %v", err)
	}
	if report.Scanned != total || report.Rewritten != total {
		t.Fatalf("report = %+v, want scanned and rewritten %d", report, total)
	}

	again, err := store.MigrateURLMetadata(ctx)
	if err != nil {
		t.Fatalf("second MigrateURLMetadata: %v", err)
	}
	if again.Scanned != total || again.Rewritten != 0 {
		t.Fatalf("second report = %+v, want scanned %d rewritten 0", again, total)
	}
}

func TestMigrateURLMetadataHonorsCancellation(t *testing.T) {
	store := openTestStorage(t, filepath.Join(t.TempDir(), "node.db"), 0)
	defer closeTestStorage(t, store)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.MigrateURLMetadata(ctx); err == nil {
		t.Fatal("expected cancellation error")
	}
}

func seedLegacyURLMetadata(t *testing.T, store *BboltStorage, row yacymodel.URIMetadataRow) {
	t.Helper()

	hash, err := row.URLHash()
	if err != nil {
		t.Fatalf("URLHash: %v", err)
	}
	err = store.update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketURLs).Put([]byte(hash), []byte(row.String()))
	})
	if err != nil {
		t.Fatalf("seed legacy url metadata: %v", err)
	}
}

func storedURLMetadata(t *testing.T, store *BboltStorage, key []byte) []byte {
	t.Helper()

	var value []byte
	err := store.view(func(tx *bolt.Tx) error {
		value = append([]byte(nil), tx.Bucket(bucketURLs).Get(key)...)
		return nil
	})
	if err != nil {
		t.Fatalf("read url metadata: %v", err)
	}

	return value
}
