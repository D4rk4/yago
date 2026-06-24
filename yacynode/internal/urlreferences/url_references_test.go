package urlreferences_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlreferences"
)

func openReferences(t *testing.T) (*boltvault.Vault, urlreferences.ReferenceProjection) {
	t.Helper()

	vault, err := boltvault.Open(filepath.Join(t.TempDir(), "node.db"), 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := vault.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	index, err := urlreferences.Open(vault)
	if err != nil {
		t.Fatalf("urlreferences.Open: %v", err)
	}

	return vault, index
}

func store(
	t *testing.T,
	vault *boltvault.Vault,
	index urlreferences.ReferenceProjection,
	word, url yacymodel.Hash,
) {
	t.Helper()

	if err := vault.Update(context.Background(), func(tx *boltvault.Txn) error {
		return index.PostingStored(tx, word, url)
	}); err != nil {
		t.Fatalf("PostingStored: %v", err)
	}
}

func purge(
	t *testing.T,
	vault *boltvault.Vault,
	index urlreferences.ReferenceProjection,
	word, url yacymodel.Hash,
) {
	t.Helper()

	if err := vault.Update(context.Background(), func(tx *boltvault.Txn) error {
		return index.PostingPurged(tx, word, url)
	}); err != nil {
		t.Fatalf("PostingPurged: %v", err)
	}
}

func wordsReferencing(
	t *testing.T,
	vault *boltvault.Vault,
	index urlreferences.ReferenceProjection,
	url yacymodel.Hash,
) []yacymodel.Hash {
	t.Helper()

	var words []yacymodel.Hash
	if err := vault.View(context.Background(), func(tx *boltvault.Txn) error {
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
	url := yacymodel.WordHash("u1")
	store(t, vault, index, yacymodel.WordHash("w1"), url)
	store(t, vault, index, yacymodel.WordHash("w2"), url)
	store(t, vault, index, yacymodel.WordHash("w3"), yacymodel.WordHash("other"))

	if got := wordsReferencing(t, vault, index, url); len(got) != 2 {
		t.Fatalf("words referencing %q = %v, want 2", url, got)
	}
}

func TestPurgedPostingForgetsURLWhenLastWordGoes(t *testing.T) {
	vault, index := openReferences(t)
	url := yacymodel.WordHash("u1")
	store(t, vault, index, yacymodel.WordHash("w1"), url)
	store(t, vault, index, yacymodel.WordHash("w2"), url)

	purge(t, vault, index, yacymodel.WordHash("w1"), url)
	if count := referencedURLCount(t, index); count != 1 {
		t.Fatalf("ReferencedURLCount after one purge = %d, want 1", count)
	}

	purge(t, vault, index, yacymodel.WordHash("w2"), url)
	if count := referencedURLCount(t, index); count != 0 {
		t.Fatalf("ReferencedURLCount after last purge = %d, want 0", count)
	}
}

func TestPurgedUnknownPostingIsHarmless(t *testing.T) {
	vault, index := openReferences(t)

	purge(t, vault, index, yacymodel.WordHash("w1"), yacymodel.WordHash("absent"))
	if count := referencedURLCount(t, index); count != 0 {
		t.Fatalf("ReferencedURLCount = %d, want 0", count)
	}
}

func TestReferencedURLCountTracksDistinctURLs(t *testing.T) {
	vault, index := openReferences(t)
	store(t, vault, index, yacymodel.WordHash("w1"), yacymodel.WordHash("u1"))
	store(t, vault, index, yacymodel.WordHash("w2"), yacymodel.WordHash("u1"))
	store(t, vault, index, yacymodel.WordHash("w1"), yacymodel.WordHash("u2"))

	if count := referencedURLCount(t, index); count != 2 {
		t.Fatalf("ReferencedURLCount = %d, want 2 distinct urls", count)
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
