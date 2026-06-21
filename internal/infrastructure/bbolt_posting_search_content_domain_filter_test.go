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
		rwiPostingWithDocType(
			yacymodel.DocTypeImage,
		)(
			yacymodel.RWIPosting{Properties: map[string]string{}},
		),
		"image",
		true,
	) {
		t.Fatal("image doctype should match strict image")
	}
	if matchesContentDomain(
		ctx,
		rwiPostingWithDocType(
			yacymodel.DocTypeAudio,
		)(
			yacymodel.RWIPosting{Properties: map[string]string{}},
		),
		"image",
		true,
	) {
		t.Fatal("audio doctype should not match strict image")
	}
	if !matchesContentDomain(
		ctx,
		rwiPostingWithDocType(
			yacymodel.DocTypeMovie,
		)(
			yacymodel.RWIPosting{Properties: map[string]string{}},
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
		rwiPostingWithFlag(
			yacymodel.RWIFlagHasAudio,
		)(
			yacymodel.RWIPosting{Properties: map[string]string{}},
		),
		"audio",
		false,
	) {
		t.Fatal("audio flag should match non-strict audio")
	}
	if matchesContentDomain(
		ctx,
		rwiPostingWithFlag(
			yacymodel.RWIFlagHasImage,
		)(
			yacymodel.RWIPosting{Properties: map[string]string{}},
		),
		"audio",
		false,
	) {
		t.Fatal("image flag should not match non-strict audio")
	}
	if !matchesContentDomain(
		ctx,
		rwiPostingWithFlag(
			yacymodel.RWIFlagHasApp,
		)(
			yacymodel.RWIPosting{Properties: map[string]string{}},
		),
		"app",
		false,
	) {
		t.Fatal("app flag should match app")
	}
}

func TestMatchesContentDomainPassthrough(t *testing.T) {
	ctx := context.Background()
	entry := rwiPostingWithDocType(
		yacymodel.DocTypeImage,
	)(
		yacymodel.RWIPosting{Properties: map[string]string{}},
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
		first  func(yacymodel.RWIPosting) yacymodel.RWIPosting
		second func(yacymodel.RWIPosting) yacymodel.RWIPosting
	}{
		{
			name: "non-strict",
			query: ports.PostingSearchQuery{
				ContentDomain: "image",
			},
			first:  rwiPostingWithFlag(yacymodel.RWIFlagHasImage),
			second: rwiPostingWithFlag(yacymodel.RWIFlagHasAudio),
		},
		{
			name: "strict",
			query: ports.PostingSearchQuery{
				ContentDomain:    "image",
				StrictContentDom: true,
			},
			first:  rwiPostingWithDocType(yacymodel.DocTypeImage),
			second: rwiPostingWithDocType(yacymodel.DocTypeAudio),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openTestStorage(t, filepath.Join(t.TempDir(), "node.db"), 0)
			defer closeTestStorage(t, store)

			word := hashForStorageTest("word")
			first := tt.first(rwiPostingForStorageTest(word, "url-a", 1))
			second := tt.second(rwiPostingForStorageTest(word, "url-b", 1))
			_, err := store.AppendRWI(ctx, []yacymodel.RWIPosting{first, second})
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
