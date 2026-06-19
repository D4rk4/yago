package infrastructure

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestMatchesSiteHash(t *testing.T) {
	const urlHash = yacymodel.Hash("0123456789AB")
	if !matchesSiteHash(urlHash, "") {
		t.Fatal("empty site hash should match")
	}
	if !matchesSiteHash(urlHash, "6789AB") {
		t.Fatal("matching host hash should match")
	}
	if matchesSiteHash(urlHash, "000000") {
		t.Fatal("non-matching host hash should not match")
	}
}

func TestBboltStorageSearchPostingsFiltersBySiteHash(t *testing.T) {
	ctx := context.Background()
	store := openTestStorage(t, filepath.Join(t.TempDir(), "node.db"), 0)
	defer closeTestStorage(t, store)

	word := hashForStorageTest("word")
	first := hashForStorageTest("url-a")
	_, err := store.AppendRWI(ctx, []yacymodel.RWIEntry{
		rwiEntryForStorageTest(word, "url-a", 1),
		rwiEntryForStorageTest(word, "url-b", 1),
	})
	if err != nil {
		t.Fatalf("AppendRWI: %v", err)
	}

	result, err := store.SearchPostings(ctx, ports.PostingSearchQuery{
		WordHashes: []yacymodel.Hash{word},
		SiteHash:   first.HostHash(),
	})
	if err != nil {
		t.Fatalf("SearchPostings: %v", err)
	}
	if result.Counts[word] != 1 {
		t.Fatalf("count = %d, want 1", result.Counts[word])
	}
	if got := singlePostingHash(t, result.Postings[word]); got != first {
		t.Fatalf("posting hash = %q, want url-a hash", got)
	}
}
