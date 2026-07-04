package urlreferences_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/urlreferences"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func openReferences(t *testing.T) (*vault.Vault, urlreferences.ReferenceProjection) {
	t.Helper()

	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	index, err := urlreferences.Open(v)
	if err != nil {
		t.Fatalf("urlreferences.Open: %v", err)
	}

	return v, index
}

func store(
	t *testing.T,
	v *vault.Vault,
	index urlreferences.ReferenceProjection,
	word, url yagomodel.Hash,
) {
	t.Helper()

	if err := v.Update(context.Background(), func(tx *vault.Txn) error {
		return index.PostingStored(tx, word, url)
	}); err != nil {
		t.Fatalf("PostingStored: %v", err)
	}
}

func purge(
	t *testing.T,
	v *vault.Vault,
	index urlreferences.ReferenceProjection,
	word, url yagomodel.Hash,
) {
	t.Helper()

	if err := v.Update(context.Background(), func(tx *vault.Txn) error {
		return index.PostingPurged(tx, word, url)
	}); err != nil {
		t.Fatalf("PostingPurged: %v", err)
	}
}

func wordsReferencing(
	t *testing.T,
	v *vault.Vault,
	index urlreferences.ReferenceProjection,
	url yagomodel.Hash,
) []yagomodel.Hash {
	t.Helper()

	var words []yagomodel.Hash
	if err := v.View(context.Background(), func(tx *vault.Txn) error {
		found, err := index.WordsReferencing(tx, url)
		if err != nil {
			return fmt.Errorf("words referencing: %w", err)
		}
		words = found

		return nil
	}); err != nil {
		t.Fatalf("WordsReferencing: %v", err)
	}

	return words
}

func TestWordsReferencingListsStoredWords(t *testing.T) {
	vault, index := openReferences(t)
	url := yagomodel.WordHash("u1")
	store(t, vault, index, yagomodel.WordHash("w1"), url)
	store(t, vault, index, yagomodel.WordHash("w2"), url)
	store(t, vault, index, yagomodel.WordHash("w3"), yagomodel.WordHash("other"))

	if got := wordsReferencing(t, vault, index, url); len(got) != 2 {
		t.Fatalf("words referencing %q = %v, want 2", url, got)
	}
}

func TestPurgedPostingForgetsURLWhenLastWordGoes(t *testing.T) {
	vault, index := openReferences(t)
	url := yagomodel.WordHash("u1")
	store(t, vault, index, yagomodel.WordHash("w1"), url)
	store(t, vault, index, yagomodel.WordHash("w2"), url)

	purge(t, vault, index, yagomodel.WordHash("w1"), url)
	if count := referencedURLCount(t, index); count != 1 {
		t.Fatalf("ReferencedURLCount after one purge = %d, want 1", count)
	}

	purge(t, vault, index, yagomodel.WordHash("w2"), url)
	if count := referencedURLCount(t, index); count != 0 {
		t.Fatalf("ReferencedURLCount after last purge = %d, want 0", count)
	}
}

func TestPurgedUnknownPostingIsHarmless(t *testing.T) {
	vault, index := openReferences(t)

	purge(t, vault, index, yagomodel.WordHash("w1"), yagomodel.WordHash("absent"))
	if count := referencedURLCount(t, index); count != 0 {
		t.Fatalf("ReferencedURLCount = %d, want 0", count)
	}
}

func TestReferencedURLCountTracksDistinctURLs(t *testing.T) {
	vault, index := openReferences(t)
	store(t, vault, index, yagomodel.WordHash("w1"), yagomodel.WordHash("u1"))
	store(t, vault, index, yagomodel.WordHash("w2"), yagomodel.WordHash("u1"))
	store(t, vault, index, yagomodel.WordHash("w1"), yagomodel.WordHash("u2"))

	if count := referencedURLCount(t, index); count != 2 {
		t.Fatalf("ReferencedURLCount = %d, want 2 distinct urls", count)
	}
}

func TestReferencedURLsListsInputURLsWithStoredReferences(t *testing.T) {
	vault, index := openReferences(t)
	first := yagomodel.WordHash("u1")
	second := yagomodel.WordHash("u2")
	store(t, vault, index, yagomodel.WordHash("w1"), first)
	store(t, vault, index, yagomodel.WordHash("w2"), first)
	store(t, vault, index, yagomodel.WordHash("w3"), second)

	got, err := index.ReferencedURLs(context.Background(), []yagomodel.Hash{
		first,
		yagomodel.WordHash("absent"),
		first,
		second,
	})
	if err != nil {
		t.Fatalf("ReferencedURLs: %v", err)
	}
	if len(got) != 2 || got[0] != first || got[1] != second {
		t.Fatalf("ReferencedURLs = %v, want %s then %s", got, first, second)
	}
}

func referencedURLCount(t *testing.T, index urlreferences.ReferenceProjection) int {
	t.Helper()

	count, err := index.ReferencedURLCount(context.Background())
	if err != nil {
		t.Fatalf("ReferencedURLCount: %v", err)
	}

	return count
}
