package infrastructure

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestMatchesContentDomainStrict(t *testing.T) {
	ctx := context.Background()
	if !matchesContentDomain(
		ctx,
		rwiEntryWithDocType(
			yacymodel.DocTypeImage,
		)(
			yacymodel.RWIEntry{Properties: map[string]string{}},
		),
		"image",
		true,
	) {
		t.Fatal("image doctype should match strict image")
	}
	if matchesContentDomain(
		ctx,
		rwiEntryWithDocType(
			yacymodel.DocTypeAudio,
		)(
			yacymodel.RWIEntry{Properties: map[string]string{}},
		),
		"image",
		true,
	) {
		t.Fatal("audio doctype should not match strict image")
	}
	if !matchesContentDomain(
		ctx,
		rwiEntryWithDocType(
			yacymodel.DocTypeMovie,
		)(
			yacymodel.RWIEntry{Properties: map[string]string{}},
		),
		"video",
		true,
	) {
		t.Fatal("movie doctype should match strict video")
	}
}

func TestMatchesContentDomainNonStrict(t *testing.T) {
	ctx := context.Background()
	if !matchesContentDomain(
		ctx,
		rwiEntryWithFlag(
			yacymodel.RWIFlagHasAudio,
		)(
			yacymodel.RWIEntry{Properties: map[string]string{}},
		),
		"audio",
		false,
	) {
		t.Fatal("audio flag should match non-strict audio")
	}
	if matchesContentDomain(
		ctx,
		rwiEntryWithFlag(
			yacymodel.RWIFlagHasImage,
		)(
			yacymodel.RWIEntry{Properties: map[string]string{}},
		),
		"audio",
		false,
	) {
		t.Fatal("image flag should not match non-strict audio")
	}
	if !matchesContentDomain(
		ctx,
		rwiEntryWithFlag(
			yacymodel.RWIFlagHasApp,
		)(
			yacymodel.RWIEntry{Properties: map[string]string{}},
		),
		"app",
		false,
	) {
		t.Fatal("app flag should match app")
	}
}

func TestMatchesContentDomainPassthrough(t *testing.T) {
	ctx := context.Background()
	entry := rwiEntryWithDocType(
		yacymodel.DocTypeImage,
	)(
		yacymodel.RWIEntry{Properties: map[string]string{}},
	)
	if !matchesContentDomain(ctx, entry, "", false) {
		t.Fatal("empty domain should pass through")
	}
	if !matchesContentDomain(ctx, entry, "text", true) {
		t.Fatal("text domain should pass through")
	}
}

func TestBboltStorageSearchPostingsFiltersByContentDomain(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		query  ports.PostingSearchQuery
		first  func(yacymodel.RWIEntry) yacymodel.RWIEntry
		second func(yacymodel.RWIEntry) yacymodel.RWIEntry
	}{
		{
			name: "non-strict",
			query: ports.PostingSearchQuery{
				ContentDomain: "image",
			},
			first:  rwiEntryWithFlag(yacymodel.RWIFlagHasImage),
			second: rwiEntryWithFlag(yacymodel.RWIFlagHasAudio),
		},
		{
			name: "strict",
			query: ports.PostingSearchQuery{
				ContentDomain:    "image",
				StrictContentDom: true,
			},
			first:  rwiEntryWithDocType(yacymodel.DocTypeImage),
			second: rwiEntryWithDocType(yacymodel.DocTypeAudio),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openTestStorage(t, filepath.Join(t.TempDir(), "node.db"), 0)
			defer closeTestStorage(t, store)

			word := hashForStorageTest("word")
			first := tt.first(rwiEntryForStorageTest(word, "url-a", 1))
			second := tt.second(rwiEntryForStorageTest(word, "url-b", 1))
			_, err := store.AppendRWI(ctx, []yacymodel.RWIEntry{first, second})
			if err != nil {
				t.Fatalf("AppendRWI: %v", err)
			}

			tt.query.WordHashes = []yacymodel.Hash{word}
			result, err := store.SearchPostings(ctx, tt.query)
			if err != nil {
				t.Fatalf("SearchPostings: %v", err)
			}
			if result.Counts[word] != 1 {
				t.Fatalf("count = %d, want 1", result.Counts[word])
			}
			if got := singlePostingHash(
				t,
				result.Postings[word],
			); got != hashForStorageTest(
				"url-a",
			) {
				t.Fatalf("posting hash = %q, want url-a hash", got)
			}
		})
	}
}
