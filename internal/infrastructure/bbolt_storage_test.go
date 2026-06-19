package infrastructure

import (
	"context"
	"strconv"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

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
