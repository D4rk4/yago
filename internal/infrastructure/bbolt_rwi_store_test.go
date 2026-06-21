package infrastructure

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestBboltStorageStoresRWIAndSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "node.db")
	store := openTestStorage(t, path, 0)

	word := hashForStorageTest("word")
	first := rwiPostingForStorageTest(word, "url-a", 1)
	duplicate := rwiPostingForStorageTest(word, "url-a", 3)
	second := rwiPostingForStorageTest(word, "url-b", 2)

	rejected, err := store.AppendRWI(ctx, []yacymodel.RWIPosting{first, second, duplicate})
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
