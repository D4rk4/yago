package infrastructure

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestBboltStorageStoresURLsAndSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "node.db")
	store := openTestStorage(t, path, 0)

	first := urlRowForStorageTest("url-a")
	second := urlRowForStorageTest("url-b")
	firstHash, err := first.URLHash()
	if err != nil {
		t.Fatalf("first URLHash: %v", err)
	}
	secondHash, err := second.URLHash()
	if err != nil {
		t.Fatalf("second URLHash: %v", err)
	}

	result, err := store.StoreURLs(ctx, []yacymodel.URIMetadataRow{first, second})
	if err != nil {
		t.Fatalf("StoreURLs: %v", err)
	}
	if len(result.Existing) != 0 || len(result.Rejected) != 0 {
		t.Fatalf("initial result = %+v, want empty", result)
	}

	result, err = store.StoreURLs(ctx, []yacymodel.URIMetadataRow{first})
	if err != nil {
		t.Fatalf("StoreURLs duplicate: %v", err)
	}
	if len(result.Existing) != 1 || result.Existing[0] != firstHash {
		t.Fatalf("existing = %v, want %v", result.Existing, firstHash)
	}

	closeTestStorage(t, store)
	store = openTestStorage(t, path, 0)
	defer closeTestStorage(t, store)

	missing, err := store.MissingURLs(ctx, []yacymodel.Hash{
		firstHash,
		hashForStorageTest("miss"),
		hashForStorageTest("miss"),
	})
	if err != nil {
		t.Fatalf("MissingURLs: %v", err)
	}
	if len(missing) != 1 || missing[0] != hashForStorageTest("miss") {
		t.Fatalf("missing = %v, want miss", missing)
	}

	rows, err := store.RowsByHash(ctx, []yacymodel.Hash{secondHash, firstHash})
	if err != nil {
		t.Fatalf("RowsByHash: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if got, _ := rows[0].URLHash(); got != secondHash {
		t.Fatalf("first row hash = %v, want %v", got, secondHash)
	}
	assertCount(t, "url count", store.URLCount, 2)
}
