package infrastructure

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func urlRowWithFreshness(seed, freshness string) yacymodel.URIMetadataRow {
	return yacymodel.URIMetadataRow{
		Properties: map[string]string{
			yacymodel.URLMetaHash: hashForStorageTest(seed).String(),
			yacymodel.ColLoadDate: freshness,
		},
	}
}

func TestSelectEvictionCandidatesReturnsStalestFirst(t *testing.T) {
	ctx := context.Background()
	store := openTestStorage(t, filepath.Join(t.TempDir(), "node.db"), 0)
	defer closeTestStorage(t, store)

	rows := []yacymodel.URIMetadataRow{
		urlRowWithFreshness("fresh", "20260101"),
		urlRowWithFreshness("stale", "20200101"),
		urlRowWithFreshness("middle", "20230101"),
	}
	if _, err := store.StoreURLs(ctx, rows); err != nil {
		t.Fatalf("StoreURLs: %v", err)
	}

	candidates, err := store.SelectEvictionCandidates(ctx, 2)
	if err != nil {
		t.Fatalf("SelectEvictionCandidates: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(candidates))
	}
	if candidates[0] != hashForStorageTest("stale") {
		t.Fatalf("candidates[0] = %v, want stale", candidates[0])
	}
	if candidates[1] != hashForStorageTest("middle") {
		t.Fatalf("candidates[1] = %v, want middle", candidates[1])
	}
}

func TestDeleteURLsRemovesPostingsAndMetadata(t *testing.T) {
	ctx := context.Background()
	store := openTestStorage(t, filepath.Join(t.TempDir(), "node.db"), 0)
	defer closeTestStorage(t, store)

	word := hashForStorageTest("word")
	if _, err := store.AppendRWI(ctx, []yacymodel.RWIEntry{
		rwiEntryForStorageTest(word, "drop-me", 1),
		rwiEntryForStorageTest(word, "keep-me", 1),
	}); err != nil {
		t.Fatalf("AppendRWI: %v", err)
	}
	if _, err := store.StoreURLs(ctx, []yacymodel.URIMetadataRow{
		urlRowForStorageTest("drop-me"),
		urlRowForStorageTest("keep-me"),
	}); err != nil {
		t.Fatalf("StoreURLs: %v", err)
	}

	result, err := store.DeleteURLs(ctx, []yacymodel.Hash{hashForStorageTest("drop-me")})
	if err != nil {
		t.Fatalf("DeleteURLs: %v", err)
	}
	if result.URLsDeleted != 1 || result.PostingsDeleted != 1 {
		t.Fatalf("result = %+v, want 1 url and 1 posting", result)
	}

	assertCount(t, "rwi count", store.RWICount, 1)
	assertCount(t, "url count", store.URLCount, 1)
	assertCount(t, "referenced url count", store.ReferencedURLCount, 1)

	missing, err := store.MissingURLs(ctx, []yacymodel.Hash{hashForStorageTest("drop-me")})
	if err != nil {
		t.Fatalf("MissingURLs: %v", err)
	}
	if len(missing) != 1 {
		t.Fatalf("missing = %v, want drop-me reported missing", missing)
	}
}

func TestUsedBytesDropsAfterDelete(t *testing.T) {
	ctx := context.Background()
	store := openTestStorage(t, filepath.Join(t.TempDir(), "node.db"), 0)
	defer closeTestStorage(t, store)

	word := hashForStorageTest("word")
	entries := make([]yacymodel.RWIEntry, 0, 512)
	rows := make([]yacymodel.URIMetadataRow, 0, 512)
	for i := range 512 {
		seed := "url-" + decimalForTest(byte(i)) + "-" + decimalForTest(byte(i/256))
		entries = append(entries, rwiEntryForStorageTest(word, seed, 1))
		rows = append(rows, urlRowForStorageTest(seed))
	}
	if _, err := store.AppendRWI(ctx, entries); err != nil {
		t.Fatalf("AppendRWI: %v", err)
	}
	if _, err := store.StoreURLs(ctx, rows); err != nil {
		t.Fatalf("StoreURLs: %v", err)
	}

	before, err := store.UsedBytes(ctx)
	if err != nil {
		t.Fatalf("UsedBytes: %v", err)
	}

	candidates, err := store.SelectEvictionCandidates(ctx, 512)
	if err != nil {
		t.Fatalf("SelectEvictionCandidates: %v", err)
	}
	if _, err := store.DeleteURLs(ctx, candidates); err != nil {
		t.Fatalf("DeleteURLs: %v", err)
	}

	after, err := store.UsedBytes(ctx)
	if err != nil {
		t.Fatalf("UsedBytes: %v", err)
	}
	if after >= before {
		t.Fatalf("used bytes after = %d, want below before = %d", after, before)
	}
}
