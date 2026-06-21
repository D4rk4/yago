package infrastructure

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestBboltStorageSearchPostingsFiltersByDistance(t *testing.T) {
	ctx := context.Background()
	store := openTestStorage(t, filepath.Join(t.TempDir(), "node.db"), 0)
	defer closeTestStorage(t, store)

	word := hashForStorageTest("word")
	near := rwiPostingForStorageTest(word, "url-a", 1)
	far := rwiPostingForStorageTest(word, "url-b", 9)
	_, err := store.AppendRWI(ctx, []yacymodel.RWIPosting{near, far})
	if err != nil {
		t.Fatalf("AppendRWI: %v", err)
	}

	result, err := store.SearchPostings(ctx, ports.PostingSearchQuery{
		WordHashes:  []yacymodel.Hash{word},
		MaxDistance: 5,
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

func TestBboltStorageSearchPostingsFiltersByLanguage(t *testing.T) {
	ctx := context.Background()
	store := openTestStorage(t, filepath.Join(t.TempDir(), "node.db"), 0)
	defer closeTestStorage(t, store)

	word := hashForStorageTest("word")
	english := rwiPostingForStorageTest(word, "url-a", 1)
	english.Properties[yacymodel.ColLanguage] = "en"
	german := rwiPostingForStorageTest(word, "url-b", 1)
	german.Properties[yacymodel.ColLanguage] = "de"
	_, err := store.AppendRWI(ctx, []yacymodel.RWIPosting{english, german})
	if err != nil {
		t.Fatalf("AppendRWI: %v", err)
	}

	result, err := store.SearchPostings(ctx, ports.PostingSearchQuery{
		WordHashes: []yacymodel.Hash{word},
		Language:   "en",
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

func TestBboltStorageSearchPostingsFiltersByURLHash(t *testing.T) {
	ctx := context.Background()
	store := openTestStorage(t, filepath.Join(t.TempDir(), "node.db"), 0)
	defer closeTestStorage(t, store)

	word := hashForStorageTest("word")
	_, err := store.AppendRWI(ctx, []yacymodel.RWIPosting{
		rwiPostingForStorageTest(word, "url-a", 1),
		rwiPostingForStorageTest(word, "url-b", 1),
		rwiPostingForStorageTest(word, "url-c", 1),
	})
	if err != nil {
		t.Fatalf("AppendRWI: %v", err)
	}

	result, err := store.SearchPostings(ctx, ports.PostingSearchQuery{
		WordHashes: []yacymodel.Hash{word},
		URLHashes: []yacymodel.Hash{
			hashForStorageTest("url-a"),
		},
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

func TestHashSet(t *testing.T) {
	if hashSet(nil) != nil {
		t.Fatal("nil input should return nil")
	}

	first := hashForStorageTest("url-a")
	second := hashForStorageTest("url-b")
	set := hashSet([]yacymodel.Hash{first, second})
	if _, ok := set[first]; !ok {
		t.Fatal("first hash missing")
	}
	if _, ok := set[second]; !ok {
		t.Fatal("second hash missing")
	}
}
