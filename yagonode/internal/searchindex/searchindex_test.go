package searchindex

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type fakeIndex struct{}

func (fakeIndex) Index(context.Context, documentstore.Document) error { return nil }

func (fakeIndex) Delete(context.Context, string) error { return nil }

func (fakeIndex) Search(context.Context, SearchRequest) (SearchResultSet, error) {
	return SearchResultSet{Results: []SearchResult{{URL: "https://example.org/"}}}, nil
}

func (fakeIndex) Stats(context.Context) (IndexStats, error) {
	return IndexStats{Documents: 1, Backend: "fake"}, nil
}

func TestSearchIndexContract(t *testing.T) {
	var index SearchIndex = fakeIndex{}

	results, err := index.Search(context.Background(), SearchRequest{Query: "example"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(results.Results))
	}
}

type fakeStoredDocuments struct {
	documents []documentstore.Document
	err       error
	scans     int
}

func (s *fakeStoredDocuments) StoredDocuments(
	_ context.Context,
	visit func(documentstore.Document) (bool, error),
) error {
	s.scans++
	if s.err != nil {
		return s.err
	}
	for _, doc := range s.documents {
		cont, err := visit(doc)
		if err != nil || !cont {
			return err
		}
	}

	return nil
}

func TestBleveMemoryIndexRebuildsAndSearchesDocuments(t *testing.T) {
	fetched := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{
			{
				NormalizedURL: "https://example.org/go",
				Title:         "Go Search",
				Headings:      []string{"Crawler"},
				ExtractedText: "Golang crawler document body.",
				Language:      "en",
				FetchedAt:     fetched,
				Inlinks:       []documentstore.AnchorText{{Text: "search anchor"}},
			},
			{
				NormalizedURL: "https://example.net/rust",
				Title:         "Rust",
				ExtractedText: "Different language.",
				Language:      "en",
			},
		},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	results, err := index.Search(t.Context(), SearchRequest{
		Query:      "golang",
		MaxResults: 5,
		IncludeRaw: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if results.Total != 1 || len(results.Results) != 1 {
		t.Fatalf("results = %#v", results)
	}
	result := results.Results[0]
	if result.DocumentID != "https://example.org/go" ||
		result.Title != "Go Search" ||
		result.URL != "https://example.org/go" ||
		result.Snippet != "Golang crawler document body." ||
		result.RawContent != "Golang crawler document body." ||
		result.PublishedDate != fetched ||
		result.Score <= 0 {
		t.Fatalf("result = %#v", result)
	}

	stats, err := index.Stats(t.Context())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Documents != 2 || stats.Backend != bleveBackendName || stats.UpdatedAt.IsZero() {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestBleveMemoryIndexUpdatesDeletesAndFilters(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	index.now = func() time.Time { return time.Date(2026, 7, 2, 11, 0, 0, 0, time.UTC) }

	doc := documentstore.Document{
		CanonicalURL:  "https://fallback.example/path/file.html",
		Title:         "Fallback document",
		ExtractedText: "Needle document text.",
		Language:      "en",
		IndexedAt:     time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := index.Index(t.Context(), doc); err != nil {
		t.Fatalf("Index: %v", err)
	}

	results, err := index.Search(t.Context(), SearchRequest{
		Query:         "needle",
		MaxResults:    1,
		IncludeDomain: []string{"example"},
		Language:      "en",
		Since:         time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		Until:         time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if results.Total != 1 || len(results.Results) != 1 {
		t.Fatalf("results = %#v", results)
	}
	if results.Results[0].URL != doc.CanonicalURL || results.Results[0].RawContent != "" {
		t.Fatalf("result = %#v", results.Results[0])
	}

	if err := index.Delete(t.Context(), doc.CanonicalURL); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	results, err = index.Search(t.Context(), SearchRequest{Query: "needle", MaxResults: 1})
	if err != nil {
		t.Fatalf("Search after delete: %v", err)
	}
	if results.Total != 0 {
		t.Fatalf("results after delete = %#v", results)
	}
}

func TestBleveMemoryIndexRejectsInvalidOperations(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	if err := index.Index(
		canceled,
		documentstore.Document{NormalizedURL: "https://example.org/"},
	); err == nil {
		t.Fatal("expected canceled index error")
	}
	if err := index.Index(t.Context(), documentstore.Document{}); err == nil {
		t.Fatal("expected missing id index error")
	}
	if err := index.Delete(canceled, "https://example.org/"); err == nil {
		t.Fatal("expected canceled delete error")
	}
	if err := index.Delete(t.Context(), " "); err == nil {
		t.Fatal("expected missing id delete error")
	}
}

func TestBleveMemoryIndexHandlesEmptyAndFailedSearches(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	for _, req := range []SearchRequest{
		{Query: "", MaxResults: 1},
		{Query: "needle", MaxResults: 0},
		{Query: "needle", MaxResults: 1},
	} {
		results, err := index.Search(t.Context(), req)
		if err != nil {
			t.Fatalf("Search(%#v): %v", req, err)
		}
		if results.Total != 0 || len(results.Results) != 0 {
			t.Fatalf("Search(%#v) = %#v", req, results)
		}
	}

	if err := index.Index(
		t.Context(),
		documentstore.Document{NormalizedURL: "https://example.org/", ExtractedText: "needle"},
	); err != nil {
		t.Fatalf("Index: %v", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := index.Search(canceled, SearchRequest{Query: "needle", MaxResults: 1}); err == nil {
		t.Fatal("expected canceled search error")
	}

	index.documents = map[string]documentstore.Document{
		"https://other.example/": {NormalizedURL: "https://other.example/"},
	}
	results, err := index.Search(t.Context(), SearchRequest{Query: "needle", MaxResults: 1})
	if err != nil {
		t.Fatalf("Search missing document: %v", err)
	}
	if results.Total != 0 {
		t.Fatalf("missing document results = %#v", results)
	}
}

func TestNewBleveMemoryIndexReturnsRebuildError(t *testing.T) {
	sentinel := errors.New("scan failed")
	_, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{err: sentinel})
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}

	_, err = NewBleveMemoryIndex(
		t.Context(),
		&fakeStoredDocuments{
			documents: []documentstore.Document{{ExtractedText: "missing id"}},
		},
	)
	if err == nil {
		t.Fatal("expected missing id rebuild error")
	}
}

func TestNewBleveMemoryIndexReturnsOpenError(t *testing.T) {
	oldNewBleveMemory := newBleveMemory
	t.Cleanup(func() { newBleveMemory = oldNewBleveMemory })
	sentinel := errors.New("open failed")
	newBleveMemory = func(mapping.IndexMapping) (bleve.Index, error) {
		return nil, sentinel
	}

	_, err := NewBleveMemoryIndex(t.Context(), nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
}

func TestBleveMemoryIndexReturnsClosedIndexErrors(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	if err := index.index.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := index.Index(
		t.Context(),
		documentstore.Document{NormalizedURL: "https://example.org/"},
	); err == nil {
		t.Fatal("expected closed index error")
	}
	if err := index.Delete(t.Context(), "https://example.org/"); err == nil {
		t.Fatal("expected closed delete error")
	}
}

func TestBleveSearchQuerySupportsExcludedTerms(t *testing.T) {
	if bleveSearchQuery(SearchRequest{Query: "golang", ExcludeTerms: []string{"", "java"}}) == nil {
		t.Fatal("expected query")
	}
}

func TestBleveMemoryIndexHelpers(t *testing.T) {
	long := strings.Repeat("å", snippetRuneCap+1)
	if got := snippet(long, "fallback"); len([]rune(got)) != snippetRuneCap {
		t.Fatalf("snippet length = %d, want %d", len([]rune(got)), snippetRuneCap)
	}
	if got := snippet(" \n\t", "fallback"); got != "fallback" {
		t.Fatalf("snippet fallback = %q", got)
	}

	doc := documentstore.Document{
		NormalizedURL: "%",
		Title:         "",
		IndexedAt:     time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
	}
	if documentHost(doc) != "" || documentTitle(doc) != "%" || documentTime(doc) != doc.IndexedAt {
		t.Fatalf(
			"helper values host=%q title=%q time=%v",
			documentHost(doc),
			documentTitle(doc),
			documentTime(doc),
		)
	}
	if domainMatches("", "example.org") || domainMatches("example.org", "") {
		t.Fatal("empty domain match should be false")
	}
	if !domainMatches("docs.example.org", ".example.org.") {
		t.Fatal("suffix domain match should be true")
	}

	req := SearchRequest{
		IncludeDomain: []string{"allowed.example"},
		ExcludeDomain: []string{"blocked.example"},
		Language:      "en",
		Since:         time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		Until:         time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC),
	}
	cases := []documentstore.Document{
		{NormalizedURL: "https://blocked.example/", Language: "en", FetchedAt: req.Since},
		{NormalizedURL: "https://other.example/", Language: "en", FetchedAt: req.Since},
		{NormalizedURL: "https://allowed.example/", Language: "fr", FetchedAt: req.Since},
		{
			NormalizedURL: "https://allowed.example/",
			Language:      "en",
			FetchedAt:     req.Since.Add(-time.Second),
		},
		{
			NormalizedURL: "https://allowed.example/",
			Language:      "en",
			FetchedAt:     req.Until.Add(time.Second),
		},
	}
	for _, doc := range cases {
		if allowsDocument(doc, req) {
			t.Fatalf("document should be rejected: %#v", doc)
		}
	}
	if !allowsDocument(
		documentstore.Document{
			NormalizedURL: "https://allowed.example/",
			Language:      "en",
			FetchedAt:     req.Since,
		},
		req,
	) {
		t.Fatal("document should be allowed")
	}
}

func TestBleveMemoryIndexPhraseBoostPrefersAdjacency(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	// Both documents carry exactly one "quick" and one "brown" in a four-word
	// body, so their term scores are identical; only `adjacent` has the two words
	// next to each other, isolating the phrase boost as the sole differentiator.
	scattered := documentstore.Document{
		CanonicalURL:  "https://scattered.example/",
		Title:         "Guide",
		ExtractedText: "brown fox quick lazy",
		Language:      "en",
	}
	adjacent := documentstore.Document{
		CanonicalURL:  "https://adjacent.example/",
		Title:         "Guide",
		ExtractedText: "quick brown fox lazy",
		Language:      "en",
	}
	for _, doc := range []documentstore.Document{scattered, adjacent} {
		if err := index.Index(t.Context(), doc); err != nil {
			t.Fatalf("Index: %v", err)
		}
	}

	results, err := index.Search(t.Context(), SearchRequest{
		Query:      "quick brown",
		Phrases:    []string{"quick brown"},
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results.Results) != 2 {
		t.Fatalf("results = %#v, want both documents", results.Results)
	}
	if results.Results[0].URL != adjacent.CanonicalURL {
		t.Fatalf("phrase match not ranked first: %#v", results.Results)
	}
}
