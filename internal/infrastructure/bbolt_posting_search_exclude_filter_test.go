package infrastructure

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestBboltStorageSearchPostingsFiltersByExcludeHash(t *testing.T) {
	ctx := context.Background()
	store := openTestStorage(t, filepath.Join(t.TempDir(), "node.db"), 0)
	defer closeTestStorage(t, store)

	word := hashForStorageTest("word")
	stop := hashForStorageTest("stop")
	_, err := store.AppendRWI(ctx, []yacymodel.RWIPosting{
		rwiPostingForStorageTest(word, "url-a", 1),
		rwiPostingForStorageTest(word, "url-b", 1),
		rwiPostingForStorageTest(stop, "url-b", 1),
	})
	if err != nil {
		t.Fatalf("AppendRWI: %v", err)
	}

	result, err := store.SearchPostings(ctx, ports.PostingSearchQuery{
		WordHashes:    []yacymodel.Hash{word},
		ExcludeHashes: []yacymodel.Hash{stop},
	})
	if err != nil {
		t.Fatalf("SearchPostings: %v", err)
	}
	if result.Counts[word] != 1 {
		t.Fatalf("count = %d, want 1", result.Counts[word])
	}
	if got := singlePostingHash(t, result.Postings[word]); got != hashForStorageTest("url-a") {
		t.Fatalf("posting hash = %q, want url-a hash", got)
	}
}
