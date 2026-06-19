package infrastructure

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

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
