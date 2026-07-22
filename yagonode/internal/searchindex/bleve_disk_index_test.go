package searchindex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type fakeDocumentDirectory struct {
	documents   map[string]documentstore.Document
	err         error
	presenceErr error
	loads       int
}

type blockingDocumentDirectory struct {
	document documentstore.Document
	started  chan struct{}
	release  chan struct{}
}

func (d *blockingDocumentDirectory) Document(
	_ context.Context,
	_ string,
) (documentstore.Document, bool, error) {
	select {
	case d.started <- struct{}{}:
	default:
	}
	<-d.release

	return d.document, true, nil
}

func (d *blockingDocumentDirectory) Count(context.Context) (int, error) {
	return 1, nil
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
	d.loads++
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

func (d *fakeDocumentDirectory) DocumentExists(
	_ context.Context,
	normalizedURL string,
) (bool, error) {
	if d.presenceErr != nil {
		return false, d.presenceErr
	}
	_, found := d.documents[normalizedURL]

	return found, nil
}

func TestBleveDiskIndexCreatesReopensAndSearches(t *testing.T) {
	fetched := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	doc := documentstore.Document{
		NormalizedURL:  "https://example.org/go",
		Title:          "Go Search",
		Headings:       []string{"Crawler"},
		ExtractedText:  "Golang crawler document body.",
		Language:       "en",
		FetchedAt:      fetched,
		PublishedAt:    fetched,
		DateConfidence: 1,
		Inlinks:        []documentstore.AnchorText{{Text: "search anchor"}},
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

func TestBleveDiskIndexAllowsIngestDuringResultHydration(t *testing.T) {
	indexed := documentstore.Document{
		NormalizedURL: "https://a.example/indexed",
		Title:         "Needle document",
		ExtractedText: "needle",
	}
	directory := &blockingDocumentDirectory{
		document: indexed,
		started:  make(chan struct{}, 1),
		release:  make(chan struct{}),
	}
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
	if err := index.Index(t.Context(), indexed); err != nil {
		t.Fatalf("Index: %v", err)
	}

	searchDone := make(chan error, 1)
	go func() {
		_, searchErr := index.Search(t.Context(), SearchRequest{Query: "needle", MaxResults: 1})
		searchDone <- searchErr
	}()
	<-directory.started
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- index.Index(t.Context(), documentstore.Document{
			NormalizedURL: "https://b.example/new",
			Title:         "New document",
			ExtractedText: "new",
		})
	}()
	select {
	case err := <-writeDone:
		if err != nil {
			close(directory.release)
			<-searchDone
			t.Fatalf("concurrent Index: %v", err)
		}
	case <-time.After(time.Second):
		close(directory.release)
		<-searchDone
		t.Fatal("ingest waited for result hydration")
	}
	close(directory.release)
	if err := <-searchDone; err != nil {
		t.Fatalf("Search: %v", err)
	}
}

func TestBleveDiskIndexUpdatesDeletesAndFilters(t *testing.T) {
	doc := documentstore.Document{
		CanonicalURL:   "https://fallback.example/path/file.html",
		Title:          "Fallback document",
		ExtractedText:  "Needle document text.",
		Language:       "en",
		IndexedAt:      time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		PublishedAt:    time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		DateConfidence: 1,
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
		Query:      "needle",
		MaxResults: 1,
		SiteHost:   "www.fallback.example",
		Language:   "en",
		Since:      time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		Until:      time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
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
	assertCompleteOrphanSearch(t, index, doc)

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
	if _, err := index.Search(
		t.Context(),
		SearchRequest{Query: "needle", MaxResults: 1, WithFacets: true},
	); err == nil {
		t.Fatal("expected complete document load error")
	}
	directory.err = nil

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := index.Search(canceled, SearchRequest{Query: "needle", MaxResults: 1}); err == nil {
		t.Fatal("expected canceled search error")
	}
}

func assertCompleteOrphanSearch(
	t *testing.T,
	index *BleveDiskIndex,
	doc documentstore.Document,
) {
	t.Helper()
	if err := index.Index(t.Context(), doc); err != nil {
		t.Fatalf("re-index missing document: %v", err)
	}
	if _, err := index.Search(t.Context(), SearchRequest{
		Query: "needle", MaxResults: 1, WithFacets: true,
	}); err != nil {
		t.Fatalf("complete orphan search: %v", err)
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

func TestBleveDiskSearchFindsEligibleTailAfterRejectedHead(t *testing.T) {
	documents := make([]documentstore.Document, 0, 6)
	for index := 0; index < 5; index++ {
		documents = append(documents, documentstore.Document{
			NormalizedURL: fmt.Sprintf("https://rejected.example/%d", index),
			Title:         "common common common common common",
			ExtractedText: "common common common common common",
			Language:      "de",
		})
	}
	documents = append(documents, documentstore.Document{
		NormalizedURL: "https://eligible.example/tail",
		ExtractedText: "common",
		Language:      "en",
	})
	index, err := NewBleveDiskIndex(
		t.Context(), filepath.Join(t.TempDir(), "tail.bleve"),
		newFakeDocumentDirectory(documents...),
		&fakeStoredDocuments{documents: documents},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	result, err := index.Search(t.Context(), SearchRequest{
		Query: "common", Language: "en", MaxResults: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || len(result.Results) != 1 ||
		result.Results[0].URL != "https://eligible.example/tail" {
		t.Fatalf("eligible tail result = %#v", result)
	}
	missing, err := index.Search(t.Context(), SearchRequest{
		Query: "absent", MaxResults: 1, WithFacets: true,
	})
	if err != nil || missing.Total != 0 {
		t.Fatalf("missing complete result = %#v, %v", missing, err)
	}
	partial, err := index.Search(t.Context(), SearchRequest{
		Query: "tail", MaxResults: 1, WithFacets: true,
	})
	if err != nil || partial.Total != 1 {
		t.Fatalf("partial complete result = %#v, %v", partial, err)
	}
}

func TestContentDomainPostFilterClassification(t *testing.T) {
	for _, contentDomain := range []string{"", "text", "TEXT", "all", "ALL"} {
		if contentDomainNeedsPostFilter(contentDomain) {
			t.Fatalf("content domain %q requires a post-filter", contentDomain)
		}
	}
	if !contentDomainNeedsPostFilter("image") {
		t.Fatal("image content domain skipped its post-filter")
	}
}

func TestBleveDiskFacetsCoverMoreThanOneThousandHits(t *testing.T) {
	documents := make([]documentstore.Document, bleveSearchHitCap+1)
	for index := range documents {
		documents[index] = documentstore.Document{
			NormalizedURL: fmt.Sprintf("https://docs.example/%04d", index),
			ExtractedText: "common searchable document",
			Language:      "en",
		}
	}
	index, err := NewBleveDiskIndex(
		t.Context(), filepath.Join(t.TempDir(), "facets.bleve"),
		newFakeDocumentDirectory(documents...),
		&fakeStoredDocuments{documents: documents},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	result, err := index.Search(t.Context(), SearchRequest{
		Query: "common", MaxResults: 1, WithFacets: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != len(documents) ||
		facetTermCounts(result.Facets, "language")["en"] != len(documents) {
		t.Fatalf("complete facet result = %#v", result)
	}
	if _, _, err := index.searchCompleteHitsWithin(
		t.Context(),
		SearchRequest{Query: "common", MaxResults: 1, WithFacets: true},
		len(documents),
		2,
	); !errors.Is(err, ErrCompleteSearchBudgetExceeded) {
		t.Fatalf("complete search budget error = %v", err)
	}
}

func TestCompleteSearchAcceptsExactHitBudget(t *testing.T) {
	documents := []documentstore.Document{
		{NormalizedURL: "https://docs.example/one", ExtractedText: "bounded match"},
		{NormalizedURL: "https://docs.example/two", ExtractedText: "bounded match"},
		{NormalizedURL: "https://docs.example/other", ExtractedText: "different text"},
	}
	index, err := NewBleveDiskIndex(
		t.Context(), filepath.Join(t.TempDir(), "boundary.bleve"),
		newFakeDocumentDirectory(documents...),
		&fakeStoredDocuments{documents: documents},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	result, _, err := index.searchCompleteHitsWithin(
		t.Context(),
		SearchRequest{Query: "bounded", MaxResults: 1, WithFacets: true},
		len(documents),
		2,
	)
	if err != nil || result.Total != 2 || len(result.Results) != 1 {
		t.Fatalf("exact-budget result = %#v, %v", result, err)
	}
}

func TestInsertCompleteHitKeepsBoundedScoreOrder(t *testing.T) {
	candidate := func(id string, score float64) *search.DocumentMatch {
		return &search.DocumentMatch{ID: id, Score: score}
	}
	results := insertCompleteHit(nil, candidate("b", 1), 2)
	results = insertCompleteHit(results, candidate("c", 0.5), 2)
	results = insertCompleteHit(results, candidate("a", 1), 2)
	results = insertCompleteHit(results, candidate("d", 0.1), 2)
	if len(results) != 2 || results[0].ID != "a" || results[1].ID != "b" {
		t.Fatalf("bounded complete results = %#v", results)
	}
	if got := insertCompleteHit(results, nil, 0); got != nil {
		t.Fatalf("zero-limit complete results = %#v", got)
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
