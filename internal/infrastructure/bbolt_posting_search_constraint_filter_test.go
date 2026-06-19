package infrastructure

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestMatchesConstraint(t *testing.T) {
	ctx := context.Background()
	entry := rwiEntryWithFlag(
		yacymodel.RWIFlagHasImage,
	)(
		yacymodel.RWIEntry{Properties: map[string]string{}},
	)

	if !matchesConstraint(ctx, entry, "") {
		t.Fatal("empty constraint should match")
	}

	allOn := yacymodel.Encode([]byte{0xff, 0xff, 0xff, 0xff})
	if !matchesConstraint(ctx, entry, allOn) {
		t.Fatal("all-on constraint is a no-op and should match")
	}

	if !matchesConstraint(ctx, entry, rwiConstraintWithFlag(yacymodel.RWIFlagHasImage)) {
		t.Fatal("constraint requiring a present flag should match")
	}

	if matchesConstraint(ctx, entry, rwiConstraintWithFlag(yacymodel.RWIFlagHasVideo)) {
		t.Fatal("constraint requiring an absent flag should not match")
	}
}

func TestBboltStorageSearchPostingsFiltersByConstraint(t *testing.T) {
	ctx := context.Background()
	store := openTestStorage(t, filepath.Join(t.TempDir(), "node.db"), 0)
	defer closeTestStorage(t, store)

	word := hashForStorageTest("word")
	_, err := store.AppendRWI(ctx, []yacymodel.RWIEntry{
		rwiEntryWithFlag(yacymodel.RWIFlagHasImage)(rwiEntryForStorageTest(word, "url-a", 1)),
		rwiEntryWithFlag(yacymodel.RWIFlagHasVideo)(rwiEntryForStorageTest(word, "url-b", 1)),
	})
	if err != nil {
		t.Fatalf("AppendRWI: %v", err)
	}

	result, err := store.SearchPostings(ctx, ports.PostingSearchQuery{
		WordHashes: []yacymodel.Hash{word},
		Constraint: rwiConstraintWithFlag(yacymodel.RWIFlagHasImage),
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
