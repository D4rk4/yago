package eviction_test

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/eviction"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
)

type postingPageScript struct {
	pages []rwi.StoredPostingPage
	err   error
	calls int
}

func (s *postingPageScript) StoredPostingPage(
	context.Context,
	[]byte,
	int,
) (rwi.StoredPostingPage, error) {
	if s.err != nil {
		return rwi.StoredPostingPage{}, s.err
	}
	if s.calls >= len(s.pages) {
		return rwi.StoredPostingPage{}, nil
	}
	page := s.pages[s.calls]
	s.calls++

	return page, nil
}

type missingURLDirectory struct {
	missing map[yagomodel.Hash]bool
	err     error
}

func (d missingURLDirectory) MissingURLs(
	_ context.Context,
	hashes []yagomodel.Hash,
) ([]yagomodel.Hash, error) {
	if d.err != nil {
		return nil, d.err
	}
	var missing []yagomodel.Hash
	for _, hash := range hashes {
		if d.missing[hash] {
			missing = append(missing, hash)
		}
	}

	return missing, nil
}

func (missingURLDirectory) RowsByHash(
	context.Context,
	[]yagomodel.Hash,
) ([]yagomodel.URIMetadataRow, error) {
	return nil, nil
}

func (missingURLDirectory) Count(context.Context) (int, error) { return 0, nil }

func TestPostingOnlyURLSourceSkipsMetadataBackedPostings(t *testing.T) {
	first := postingForURL("alpha", "https://example.com/known")
	second := postingForURL("beta", "https://example.com/orphan")
	orphan, err := second.URLHash()
	if err != nil {
		t.Fatal(err)
	}
	pages := &postingPageScript{pages: []rwi.StoredPostingPage{{
		Entries: []rwi.StoredPosting{
			{Cursor: []byte("a"), Posting: first},
			{Cursor: []byte("b"), Posting: second},
		},
	}}}
	source := eviction.NewPostingOnlyURLSource(pages, missingURLDirectory{
		missing: map[yagomodel.Hash]bool{orphan.Hash(): true},
	})
	got, err := source.PostingOnlyURLs(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != orphan.Hash() {
		t.Fatalf("posting-only urls = %v", got)
	}
}

func TestPostingOnlyURLSourcePropagatesPageAndDirectoryFailures(t *testing.T) {
	want := errors.New("failed")
	if _, err := eviction.NewPostingOnlyURLSource(
		&postingPageScript{err: want}, missingURLDirectory{},
	).PostingOnlyURLs(context.Background(), 1); !errors.Is(err, want) {
		t.Fatalf("page error = %v", err)
	}
	posting := postingForURL("alpha", "https://example.com/")
	if _, err := eviction.NewPostingOnlyURLSource(
		&postingPageScript{pages: []rwi.StoredPostingPage{{
			Entries: []rwi.StoredPosting{{Cursor: []byte("a"), Posting: posting}},
		}}},
		missingURLDirectory{err: want},
	).PostingOnlyURLs(context.Background(), 1); !errors.Is(err, want) {
		t.Fatalf("directory error = %v", err)
	}
}

func TestPostingOnlyURLSourceRequiresBothDirectoriesAndPositiveLimit(t *testing.T) {
	t.Parallel()

	pages := &postingPageScript{}
	urls := missingURLDirectory{}
	if eviction.NewPostingOnlyURLSource(nil, urls) != nil {
		t.Fatal("source accepted a nil posting directory")
	}
	if eviction.NewPostingOnlyURLSource(pages, nil) != nil {
		t.Fatal("source accepted a nil URL directory")
	}
	source := eviction.NewPostingOnlyURLSource(pages, urls)
	got, err := source.PostingOnlyURLs(context.Background(), 0)
	if err != nil || got != nil || pages.calls != 0 {
		t.Fatalf("zero-limit result = %v, %v; calls = %d", got, err, pages.calls)
	}
}

func TestPostingOnlyURLSourceBoundsAndAdvancesCandidatePages(t *testing.T) {
	t.Parallel()

	known := postingForURL("alpha", "https://example.com/known")
	orphan := postingForURL("beta", "https://example.com/orphan")
	orphanSecond := postingForURL("gamma", "https://example.com/orphan-second")
	orphanHash, err := orphan.URLHash()
	if err != nil {
		t.Fatal(err)
	}
	orphanSecondHash, err := orphanSecond.URLHash()
	if err != nil {
		t.Fatal(err)
	}
	pages := &postingPageScript{pages: []rwi.StoredPostingPage{
		{
			Entries: []rwi.StoredPosting{
				{Cursor: []byte("a"), Posting: known},
				{Cursor: []byte("b"), Posting: known},
			},
			More: true,
		},
		{
			Entries: []rwi.StoredPosting{
				{Cursor: []byte("c"), Posting: orphan},
				{Cursor: []byte("d"), Posting: orphanSecond},
			},
		},
	}}
	source := eviction.NewPostingOnlyURLSource(pages, missingURLDirectory{
		missing: map[yagomodel.Hash]bool{
			orphanHash.Hash():       true,
			orphanSecondHash.Hash(): true,
		},
	})
	got, err := source.PostingOnlyURLs(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != orphanHash.Hash() || pages.calls != 2 {
		t.Fatalf("bounded candidates = %v; calls = %d", got, pages.calls)
	}
	got, err = source.PostingOnlyURLs(context.Background(), 1)
	if err != nil || got != nil || pages.calls != 2 {
		t.Fatalf("finished scan = %v, %v; calls = %d", got, err, pages.calls)
	}
}

func TestPostingOnlyURLSourceRejectsMalformedPosting(t *testing.T) {
	t.Parallel()

	source := eviction.NewPostingOnlyURLSource(
		&postingPageScript{pages: []rwi.StoredPostingPage{{
			Entries: []rwi.StoredPosting{{
				Cursor:  []byte("a"),
				Posting: yagomodel.RWIPosting{Properties: map[string]string{}},
			}},
		}}},
		missingURLDirectory{},
	)
	if _, err := source.PostingOnlyURLs(context.Background(), 1); err == nil {
		t.Fatal("malformed posting accepted")
	}
}

func TestPostingOnlyURLSourceStopsAtInspectionBound(t *testing.T) {
	t.Parallel()

	entries := make([]rwi.StoredPosting, 4096)
	for index := range entries {
		entries[index] = rwi.StoredPosting{
			Cursor:  []byte{byte(index >> 8), byte(index)},
			Posting: postingForURL("alpha", "https://example.com/known"),
		}
	}
	pages := &postingPageScript{pages: []rwi.StoredPostingPage{{
		Entries: entries,
		More:    true,
	}}}
	source := eviction.NewPostingOnlyURLSource(pages, missingURLDirectory{})
	got, err := source.PostingOnlyURLs(context.Background(), 4096)
	if err != nil || got != nil || pages.calls != 1 {
		t.Fatalf("inspection-bound result = %v, %v; calls = %d", got, err, pages.calls)
	}
}

func postingForURL(word, rawURL string) yagomodel.RWIPosting {
	hash, _ := yagomodel.HashURL(rawURL)

	return yagomodel.RWIPosting{
		WordHash: yagomodel.WordHash(word),
		Properties: map[string]string{
			yagomodel.ColURLHash: hash.String(),
		},
	}
}
