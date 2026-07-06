package searchindex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type fakeDocumentDirectory struct {
	documents map[string]documentstore.Document
	err       error
}

func newFakeDocumentDirectory(docs ...documentstore.Document) *fakeDocumentDirectory {
	out := &fakeDocumentDirectory{documents: map[string]documentstore.Document{}}
	for _, doc := range docs {
		out.documents[documentID(doc)] = doc
	}

	return out
}

func (d *fakeDocumentDirectory) Document(
	_ context.Context,
	normalizedURL string,
) (documentstore.Document, bool, error) {
	if d.err != nil {
		return documentstore.Document{}, false, d.err
	}
	doc, found := d.documents[normalizedURL]
	return doc, found, nil
}

func (d *fakeDocumentDirectory) Count(context.Context) (int, error) {
	if d.err != nil {
		return 0, d.err
	}

	return len(d.documents), nil
}

func TestBleveDiskIndexCreatesReopensAndSearches(t *testing.T) {
	fetched := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	doc := documentstore.Document{
		NormalizedURL: "https://example.org/go",
		Title:         "Go Search",
		Headings:      []string{"Crawler"},
		ExtractedText: "Golang crawler document body.",
		Language:      "en",
		FetchedAt:     fetched,
		Inlinks:       []documentstore.AnchorText{{Text: "search anchor"}},
	}
	directory := newFakeDocumentDirectory(doc)
	stored := &fakeStoredDocuments{documents: []documentstore.Document{doc}}
	indexPath := filepath.Join(t.TempDir(), "search.bleve")
	index, err := NewBleveDiskIndex(
		t.Context(),
		indexPath,
		directory,
		stored,
	)
	if err != nil {
		t.Fatalf("NewBleveDiskIndex: %v", err)
	}
	index.now = func() time.Time { return time.Date(2026, 7, 2, 12, 0, 0, 0, time.FixedZone("x", 3600)) }
	if err := index.Index(t.Context(), doc); err != nil {
		t.Fatalf("Index: %v", err)
	}
	stats, err := index.Stats(t.Context())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Documents != 1 ||
		stats.Backend != bleveDiskBackendName ||
		stats.UpdatedAt != time.Date(2026, 7, 2, 11, 0, 0, 0, time.UTC) ||
		stored.scans != 1 {
		t.Fatalf("stats = %#v scans=%d", stats, stored.scans)
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
	if result.DocumentID != doc.NormalizedURL ||
		result.Title != doc.Title ||
		result.RawContent != doc.ExtractedText ||
		result.PublishedDate != fetched ||
		result.Score <= 0 {
		t.Fatalf("result = %#v", result)
	}

	path := indexPath
	if err := index.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopenedStored := &fakeStoredDocuments{err: errors.New("should not scan")}
	reopened, err := NewBleveDiskIndex(t.Context(), path, directory, reopenedStored)
	if err != nil {
		t.Fatalf("reopen disk index: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if reopenedStored.scans != 0 {
		t.Fatalf("reopen scanned documents: %d", reopenedStored.scans)
	}
	reopenedStats, err := reopened.Stats(t.Context())
	if err != nil {
		t.Fatalf("reopened Stats: %v", err)
	}
	if reopenedStats.Documents != 1 || reopenedStats.UpdatedAt.IsZero() {
		t.Fatalf("reopened stats = %#v", reopenedStats)
	}
}

func TestBleveDiskIndexUpdatesDeletesAndFilters(t *testing.T) {
	doc := documentstore.Document{
		CanonicalURL:  "https://fallback.example/path/file.html",
		Title:         "Fallback document",
		ExtractedText: "Needle document text.",
		Language:      "en",
		IndexedAt:     time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	}
	directory := newFakeDocumentDirectory(doc)
	index, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "search.bleve"),
		directory,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBleveDiskIndex: %v", err)
	}
	t.Cleanup(func() { _ = index.Close() })
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

func TestBleveDiskIndexRepairsUnreadableIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "search.bleve")
	if err := os.WriteFile(path, []byte("not an index"), 0o600); err != nil {
		t.Fatalf("write broken index: %v", err)
	}
	doc := documentstore.Document{
		NormalizedURL: "https://example.org/repaired",
		ExtractedText: "Repaired index document.",
	}
	index, err := NewBleveDiskIndex(
		t.Context(),
		path,
		newFakeDocumentDirectory(doc),
		&fakeStoredDocuments{documents: []documentstore.Document{doc}},
	)
	if err != nil {
		t.Fatalf("repair disk index: %v", err)
	}
	t.Cleanup(func() { _ = index.Close() })

	results, err := index.Search(t.Context(), SearchRequest{Query: "repaired", MaxResults: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if results.Total != 1 {
		t.Fatalf("results = %#v", results)
	}
}

func TestNewBleveDiskIndexRejectsInvalidInputs(t *testing.T) {
	if _, err := NewBleveDiskIndex(t.Context(), " ", newFakeDocumentDirectory(), nil); err == nil {
		t.Fatal("expected missing path error")
	}
	if _, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "search.bleve"),
		nil,
		nil,
	); err == nil {
		t.Fatal("expected missing directory error")
	}
	if _, err := NewBleveDiskIndex(
		t.Context(),
		string([]byte{'b', 0}),
		newFakeDocumentDirectory(),
		nil,
	); err == nil {
		t.Fatal("expected stat error")
	}
}

func TestNewBleveDiskIndexReturnsOpenCreateAndRebuildErrors(t *testing.T) {
	sentinel := errors.New("failed")
	directory := newFakeDocumentDirectory()

	oldNewBleveDisk := newBleveDisk
	t.Cleanup(func() { newBleveDisk = oldNewBleveDisk })
	newBleveDisk = func(string, mapping.IndexMapping) (bleve.Index, error) {
		return nil, sentinel
	}
	if _, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "search.bleve"),
		directory,
		nil,
	); !errors.Is(err, sentinel) {
		t.Fatalf("create error = %v, want %v", err, sentinel)
	}
	newBleveDisk = oldNewBleveDisk

	if _, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "search.bleve"),
		directory,
		&fakeStoredDocuments{err: sentinel},
	); !errors.Is(err, sentinel) {
		t.Fatalf("rebuild error = %v, want %v", err, sentinel)
	}
	if _, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "search.bleve"),
		directory,
		&fakeStoredDocuments{
			documents: []documentstore.Document{{ExtractedText: "missing id"}},
		},
	); err == nil {
		t.Fatal("expected rebuild document error")
	}
}

func TestNewBleveDiskIndexReturnsRepairErrors(t *testing.T) {
	sentinel := errors.New("failed")
	path := filepath.Join(t.TempDir(), "search.bleve")
	if err := os.WriteFile(path, []byte("not an index"), 0o600); err != nil {
		t.Fatalf("write broken index: %v", err)
	}

	oldOpenBleveDisk := openBleveDisk
	oldRemoveBleveDisk := removeBleveDisk
	oldNewBleveDisk := newBleveDisk
	t.Cleanup(func() {
		openBleveDisk = oldOpenBleveDisk
		removeBleveDisk = oldRemoveBleveDisk
		newBleveDisk = oldNewBleveDisk
	})

	openBleveDisk = func(string) (bleve.Index, error) { return nil, sentinel }
	removeBleveDisk = func(string) error { return sentinel }
	if _, err := NewBleveDiskIndex(
		t.Context(),
		path,
		newFakeDocumentDirectory(),
		nil,
	); !errors.Is(
		err,
		sentinel,
	) {
		t.Fatalf("remove error = %v, want %v", err, sentinel)
	}

	removeBleveDisk = oldRemoveBleveDisk
	newBleveDisk = func(string, mapping.IndexMapping) (bleve.Index, error) {
		return nil, sentinel
	}
	if _, err := NewBleveDiskIndex(
		t.Context(),
		path,
		newFakeDocumentDirectory(),
		nil,
	); !errors.Is(
		err,
		sentinel,
	) {
		t.Fatalf("recreate error = %v, want %v", err, sentinel)
	}
}

func TestBleveDiskIndexRejectsInvalidOperations(t *testing.T) {
	index, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "search.bleve"),
		newFakeDocumentDirectory(),
		nil,
	)
	if err != nil {
		t.Fatalf("NewBleveDiskIndex: %v", err)
	}
	t.Cleanup(func() { _ = index.Close() })
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
	if _, err := index.Stats(canceled); err == nil {
		t.Fatal("expected canceled stats error")
	}
}

func TestBleveDiskIndexHandlesEmptyMissingAndFailedSearches(t *testing.T) {
	directory := newFakeDocumentDirectory()
	index, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "search.bleve"),
		directory,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBleveDiskIndex: %v", err)
	}
	t.Cleanup(func() { _ = index.Close() })
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

	doc := documentstore.Document{
		NormalizedURL: "https://example.org/",
		ExtractedText: "needle",
	}
	if err := index.Index(t.Context(), doc); err != nil {
		t.Fatalf("Index: %v", err)
	}
	results, err := index.Search(t.Context(), SearchRequest{Query: "needle", MaxResults: 1})
	if err != nil {
		t.Fatalf("Search missing document: %v", err)
	}
	if results.Total != 0 {
		t.Fatalf("missing document results = %#v", results)
	}
	// The orphaned entry self-heals: the search that discovered the vanished
	// document also removed its index entry.
	stats, err := index.Stats(t.Context())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Documents != 0 {
		t.Fatalf("orphaned entry not healed, documents = %d", stats.Documents)
	}

	directory.documents[doc.NormalizedURL] = doc
	if err := index.Index(t.Context(), doc); err != nil {
		t.Fatalf("re-index healed document: %v", err)
	}
	directory.err = errors.New("document failed")
	if _, err := index.Search(
		t.Context(),
		SearchRequest{Query: "needle", MaxResults: 1},
	); err == nil {
		t.Fatal("expected document load error")
	}
	directory.err = nil

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := index.Search(canceled, SearchRequest{Query: "needle", MaxResults: 1}); err == nil {
		t.Fatal("expected canceled search error")
	}
}

func TestBleveDiskIndexReturnsClosedIndexErrors(t *testing.T) {
	index, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "search.bleve"),
		newFakeDocumentDirectory(),
		nil,
	)
	if err != nil {
		t.Fatalf("NewBleveDiskIndex: %v", err)
	}
	if err := index.Close(); err != nil {
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
	if _, err := index.Search(
		t.Context(),
		SearchRequest{Query: "example", MaxResults: 1},
	); err == nil {
		t.Fatal("expected closed search error")
	}
	if _, err := index.Stats(t.Context()); err == nil {
		t.Fatal("expected closed stats error")
	}
	if err := index.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestBleveDiskIndexReturnsUnexpectedUnderlyingCloseErrors(t *testing.T) {
	index, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "search.bleve"),
		newFakeDocumentDirectory(),
		nil,
	)
	if err != nil {
		t.Fatalf("NewBleveDiskIndex: %v", err)
	}
	closeBleveShards(index.shards)

	if err := index.Index(
		t.Context(),
		documentstore.Document{NormalizedURL: "https://example.org/"},
	); err == nil {
		t.Fatal("expected underlying index error")
	}
	if err := index.Delete(t.Context(), "https://example.org/"); err == nil {
		t.Fatal("expected underlying delete error")
	}
	if _, err := index.Search(
		t.Context(),
		SearchRequest{Query: "example", MaxResults: 1},
	); err == nil {
		t.Fatal("expected underlying search count error")
	}
	if _, err := index.Stats(t.Context()); err == nil {
		t.Fatal("expected underlying stats count error")
	}
}

func TestBleveDiskIndexWrapsCloseErrors(t *testing.T) {
	sentinel := errors.New("close failed")
	index := &BleveDiskIndex{shards: []bleve.Index{closeErrorBleveIndex{err: sentinel}}}

	if err := index.Close(); !errors.Is(err, sentinel) {
		t.Fatalf("Close error = %v, want %v", err, sentinel)
	}
}

type closeErrorBleveIndex struct {
	bleveIndexContract
	err error
}

func (i closeErrorBleveIndex) Close() error {
	return i.err
}

type bleveIndexContract interface {
	bleve.Index
}

func TestDiskSearchSize(t *testing.T) {
	cases := map[string]struct {
		maxResults int
		documents  int
		want       int
	}{
		"negative": {-1, 10, 0},
		"zero":     {0, 10, 0},
		"small":    {5, 100, 20},
		"cap":      {500, 3000, bleveSearchHitCap},
		"docs":     {500, 7, 7},
	}
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			if got := diskSearchSize(tt.maxResults, tt.documents); got != tt.want {
				t.Fatalf("diskSearchSize = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBleveDocumentCount(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	if got := bleveDocumentCount(7); got != 7 {
		t.Fatalf("small count = %d, want 7", got)
	}
	if got := bleveDocumentCount(uint64(maxInt) + 1); got != maxInt {
		t.Fatalf("large count = %d, want %d", got, maxInt)
	}
}

func TestDropOrphanedEntriesStopsOnDeleteError(t *testing.T) {
	directory := newFakeDocumentDirectory()
	index, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "search.bleve"),
		directory,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBleveDiskIndex: %v", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	index.dropOrphanedEntries(canceled, []string{"https://example.org/"})
}

func TestBleveDiskIndexSearchFiltersDisallowedDocuments(t *testing.T) {
	doc := documentstore.Document{
		NormalizedURL: "https://example.org/rejected",
		ExtractedText: "needle body",
		Language:      "de",
	}
	index, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "search.bleve"),
		newFakeDocumentDirectory(doc),
		nil,
	)
	if err != nil {
		t.Fatalf("NewBleveDiskIndex: %v", err)
	}
	t.Cleanup(func() { _ = index.Close() })
	if err := index.Index(t.Context(), doc); err != nil {
		t.Fatalf("Index: %v", err)
	}
	results, err := index.Search(t.Context(), SearchRequest{
		Query:      "needle",
		MaxResults: 1,
		Language:   "ru",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if results.Total != 0 {
		t.Fatalf("language-filtered results = %#v", results)
	}
	stats, err := index.Stats(t.Context())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Documents != 1 {
		t.Fatalf("disallowed document treated as orphan, documents = %d", stats.Documents)
	}
}
