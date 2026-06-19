package infrastructure

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestBboltStorageStoresRWIAndSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "node.db")
	store := openTestStorage(t, path, 0)

	word := hashForStorageTest("word")
	first := rwiEntryForStorageTest(word, "url-a", 1)
	duplicate := rwiEntryForStorageTest(word, "url-a", 3)
	second := rwiEntryForStorageTest(word, "url-b", 2)

	rejected, err := store.AppendRWI(ctx, []yacymodel.RWIEntry{first, second, duplicate})
	if err != nil {
		t.Fatalf("AppendRWI: %v", err)
	}
	if len(rejected) != 0 {
		t.Fatalf("rejected = %v, want none", rejected)
	}
	assertCount(t, "rwi count", store.RWICount, 2)
	assertCount(t, "referenced url count", store.ReferencedURLCount, 2)

	closeTestStorage(t, store)
	store = openTestStorage(t, path, 0)
	defer closeTestStorage(t, store)

	postings, err := store.SearchPostings(ctx, ports.PostingSearchQuery{
		WordHashes: []yacymodel.Hash{word},
	})
	if err != nil {
		t.Fatalf("SearchPostings: %v", err)
	}
	if len(postings.Postings[word]) != 2 {
		t.Fatalf("postings = %d, want 2", len(postings.Postings[word]))
	}
	if postings.Postings[word][0].Properties[yacymodel.ColWordDistance] != decimalForTest(3) {
		t.Fatalf("duplicate did not overwrite first posting: %v", postings.Postings[word][0])
	}
	assertCount(t, "rwi count after reopen", store.RWICount, 2)
	assertCount(t, "referenced url count after reopen", store.ReferencedURLCount, 2)
}

func TestBboltStorageSearchPostingsBoundsPerWord(t *testing.T) {
	ctx := context.Background()
	store := openTestStorage(t, filepath.Join(t.TempDir(), "node.db"), 0)
	defer closeTestStorage(t, store)

	word := hashForStorageTest("word")
	other := hashForStorageTest("other")
	_, err := store.AppendRWI(ctx, []yacymodel.RWIEntry{
		rwiEntryForStorageTest(word, "url-a", 1),
		rwiEntryForStorageTest(word, "url-b", 2),
		rwiEntryForStorageTest(other, "url-c", 3),
	})
	if err != nil {
		t.Fatalf("AppendRWI: %v", err)
	}

	postings, err := store.SearchPostings(ctx, ports.PostingSearchQuery{
		WordHashes:   []yacymodel.Hash{word, other},
		LimitPerWord: 1,
	})
	if err != nil {
		t.Fatalf("SearchPostings: %v", err)
	}
	if len(postings.Postings[word]) != 1 {
		t.Fatalf("bounded postings = %d, want 1", len(postings.Postings[word]))
	}
	if len(postings.Postings[other]) != 1 {
		t.Fatalf("other postings = %d, want 1", len(postings.Postings[other]))
	}
	if !postings.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if postings.Counts[word] != 2 {
		t.Fatalf("word count = %d, want 2", postings.Counts[word])
	}
}

func TestBboltStorageSearchPostingsFilters(t *testing.T) {
	ctx := context.Background()
	store := openTestStorage(t, filepath.Join(t.TempDir(), "node.db"), 0)
	defer closeTestStorage(t, store)

	word := hashForStorageTest("word")
	stop := hashForStorageTest("stop")
	english := rwiEntryForStorageTest(word, "url-a", 1)
	english.Properties[yacymodel.ColLanguage] = "en"
	german := rwiEntryForStorageTest(word, "url-b", 1)
	german.Properties[yacymodel.ColLanguage] = "de"
	far := rwiEntryForStorageTest(word, "url-c", 9)
	far.Properties[yacymodel.ColLanguage] = "en"
	excluded := rwiEntryForStorageTest(stop, "url-a", 1)
	_, err := store.AppendRWI(ctx, []yacymodel.RWIEntry{english, german, far, excluded})
	if err != nil {
		t.Fatalf("AppendRWI: %v", err)
	}

	result, err := store.SearchPostings(ctx, ports.PostingSearchQuery{
		WordHashes:    []yacymodel.Hash{word},
		ExcludeHashes: []yacymodel.Hash{stop},
		URLHashes:     []yacymodel.Hash{hashForStorageTest("url-a"), hashForStorageTest("url-c")},
		MaxDistance:   5,
		Language:      "en",
	})
	if err != nil {
		t.Fatalf("SearchPostings: %v", err)
	}
	if result.Counts[word] != 0 {
		t.Fatalf("count = %d, want 0", result.Counts[word])
	}
	if len(result.Postings[word]) != 0 {
		t.Fatalf("postings = %d, want 0", len(result.Postings[word]))
	}
}

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

func TestBboltStorageRejectsAtCapacity(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "node.db")
	store := openTestStorage(t, path, 1)
	defer closeTestStorage(t, store)

	_, err := store.StoreURLs(ctx, []yacymodel.URIMetadataRow{urlRowForStorageTest("url-a")})
	if !errors.Is(err, ports.ErrAtCapacity) {
		t.Fatalf("StoreURLs error = %v, want ErrAtCapacity", err)
	}

	_, err = store.AppendRWI(
		ctx,
		[]yacymodel.RWIEntry{rwiEntryForStorageTest(hashForStorageTest("word"), "url-a", 1)},
	)
	if !errors.Is(err, ports.ErrAtCapacity) {
		t.Fatalf("AppendRWI error = %v, want ErrAtCapacity", err)
	}
	assertCount(t, "rwi count", store.RWICount, 0)
	assertCount(t, "url count", store.URLCount, 0)
}

func TestBboltStorageMapsCapacityWriteErrors(t *testing.T) {
	err := wrapStorageError("write storage", syscall.ENOSPC)
	if !errors.Is(err, ports.ErrAtCapacity) {
		t.Fatalf("mapped error = %v, want ErrAtCapacity", err)
	}

	err = wrapStorageError(
		"write storage",
		errors.New("file resize error: no space left on device"),
	)
	if !errors.Is(err, ports.ErrAtCapacity) {
		t.Fatalf("string mapped error = %v, want ErrAtCapacity", err)
	}
}

func TestBboltStorageHidesImplementationWriteErrors(t *testing.T) {
	err := wrapStorageError("write storage", errors.New("bbolt internal detail"))
	if !errors.Is(err, ports.ErrStoreFailure) {
		t.Fatalf("mapped error = %v, want ErrStoreFailure", err)
	}
	if errors.Is(err, ports.ErrAtCapacity) {
		t.Fatalf("mapped error = %v, did not want ErrAtCapacity", err)
	}
}

func openTestStorage(t *testing.T, path string, quotaBytes int64) *BboltStorage {
	t.Helper()

	store, err := OpenBboltStorage(path, quotaBytes)
	if err != nil {
		t.Fatalf("OpenBboltStorage: %v", err)
	}

	return store
}

func closeTestStorage(t *testing.T, store *BboltStorage) {
	t.Helper()

	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func assertCount(
	t *testing.T,
	name string,
	count func(context.Context) (int, error),
	want int,
) {
	t.Helper()

	got, err := count(context.Background())
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if got != want {
		t.Fatalf("%s = %d, want %d", name, got, want)
	}
}

func hashForStorageTest(seed string) yacymodel.Hash {
	return yacymodel.WordHash(seed)
}

func rwiEntryForStorageTest(
	word yacymodel.Hash,
	urlSeed string,
	distance byte,
) yacymodel.RWIEntry {
	return yacymodel.RWIEntry{
		WordHash: word,
		Properties: map[string]string{
			yacymodel.ColURLHash:        hashForStorageTest(urlSeed).String(),
			yacymodel.ColLocalLinkCount: decimalForTest(1),
			yacymodel.ColWordDistance:   decimalForTest(distance),
		},
	}
}

func urlRowForStorageTest(seed string) yacymodel.URIMetadataRow {
	return yacymodel.URIMetadataRow{
		Properties: map[string]string{
			yacymodel.URLMetaHash: hashForStorageTest(seed).String(),
		},
	}
}

func decimalForTest(value byte) string {
	return strconv.FormatUint(uint64(value), 10)
}
