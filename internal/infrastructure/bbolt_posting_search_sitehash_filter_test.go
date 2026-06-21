package infrastructure

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestMatchesSiteHash(t *testing.T) {
	const urlHash = yacymodel.URLHash("0123456789AB")
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
	firstHash := hashForStorageTest("url-a")
	first := yacymodel.URLHash(firstHash.String())
	_, err := store.AppendRWI(ctx, []yacymodel.RWIPosting{
		rwiPostingForStorageTest(word, "url-a", 1),
		rwiPostingForStorageTest(word, "url-b", 1),
	})
	if err != nil {
		t.Fatalf("AppendRWI: %v", err)
	}

	result, err := store.SearchPostings(ctx, ports.PostingSearchQuery{
		WordHashes: []yacymodel.Hash{word},
		SiteHash: func() string {
			got, err := yacymodel.URLHash(first.String()).HostHash()
			if err != nil {
				t.Fatalf("HostHash: %v", err)
			}
			return got
		}(),
	})
	if err != nil {
		t.Fatalf("SearchPostings: %v", err)
	}
	if result.Counts[word] != 1 {
		t.Fatalf("count = %d, want 1", result.Counts[word])
	}
	if got := singlePostingHash(t, result.Postings[word]); got != firstHash {
		t.Fatalf("posting hash = %q, want url-a hash", got)
	}
}
