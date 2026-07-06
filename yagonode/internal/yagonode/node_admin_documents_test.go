package yagonode

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type fakeStoredDocuments struct {
	docs   []documentstore.Document
	err    error
	visits *int
}

func (f fakeStoredDocuments) StoredDocuments(
	_ context.Context,
	visit func(documentstore.Document) (bool, error),
) error {
	for _, doc := range f.docs {
		if f.visits != nil {
			*f.visits++
		}
		keep, err := visit(doc)
		if err != nil {
			return err
		}
		if !keep {
			return nil
		}
	}

	return f.err
}

func day(n int) time.Time {
	return time.Date(2026, 7, n, 0, 0, 0, 0, time.UTC)
}

func TestBrowseDocumentsFiltersAndSorts(t *testing.T) {
	docs := []documentstore.Document{
		{
			CanonicalURL: "https://a.example/page1",
			Title:        "One",
			ContentType:  "text/html",
			Language:     "en",
			IndexedAt:    day(2),
		},
		{CanonicalURL: "https://b.example/page2", Title: "Two", IndexedAt: day(1)},
		{CanonicalURL: "https://sub.a.example/deep", Title: "Three", IndexedAt: day(3)},
	}
	source := newDocumentBrowseSource(fakeStoredDocuments{docs: docs})
	ctx := context.Background()

	all := source.BrowseDocuments(ctx, adminui.DocumentQuery{})
	if all.Matched != 3 || len(all.Documents) != 3 {
		t.Fatalf("browse all = %+v", all)
	}
	if all.Documents[0].Title != "Three" || all.Documents[2].Title != "Two" {
		t.Fatalf("documents should be newest-indexed first: %+v", all.Documents)
	}

	byURL := source.BrowseDocuments(ctx, adminui.DocumentQuery{URLContains: "PAGE1"})
	if byURL.Matched != 1 || byURL.Documents[0].Title != "One" {
		t.Fatalf("URL substring filter (case-insensitive) = %+v", byURL)
	}

	byDomain := source.BrowseDocuments(ctx, adminui.DocumentQuery{Domain: "a.example"})
	if byDomain.Matched != 2 {
		t.Fatalf("domain filter should match the host and its subdomains: %+v", byDomain)
	}
}

func TestBrowseDocumentsTruncatesToLimit(t *testing.T) {
	docs := make([]documentstore.Document, 0, documentBrowseLimit+5)
	for i := 0; i < documentBrowseLimit+5; i++ {
		docs = append(docs, documentstore.Document{
			CanonicalURL: fmt.Sprintf("https://x.example/%d", i),
			IndexedAt:    day(1),
		})
	}
	visits := 0
	page := newDocumentBrowseSource(fakeStoredDocuments{docs: docs, visits: &visits}).
		BrowseDocuments(context.Background(), adminui.DocumentQuery{})

	if !page.Sampled || len(page.Documents) != documentBrowseLimit {
		t.Fatalf("expected an early-stopped full page: %+v", page)
	}
	if visits != documentBrowseLimit {
		t.Fatalf("unfiltered browse visited %d documents, want exactly %d (PERF-01)",
			visits, documentBrowseLimit)
	}
}

// TestBrowseDocumentsFilteredScanHitsBudget pins the PERF-01 bound: a filter
// matching nothing must stop at the scan budget instead of decoding the whole
// store, and the page says so.
func TestBrowseDocumentsFilteredScanHitsBudget(t *testing.T) {
	docs := make([]documentstore.Document, 0, documentScanBudget+10)
	for i := 0; i < documentScanBudget+10; i++ {
		docs = append(docs, documentstore.Document{
			CanonicalURL: fmt.Sprintf("https://x.example/%d", i),
			IndexedAt:    day(1),
		})
	}
	visits := 0
	page := newDocumentBrowseSource(fakeStoredDocuments{docs: docs, visits: &visits}).
		BrowseDocuments(context.Background(), adminui.DocumentQuery{Domain: "absent.example"})

	if !page.Sampled || len(page.Documents) != 0 {
		t.Fatalf("expected a sampled empty page: %+v", page)
	}
	if visits != documentScanBudget+1 {
		t.Fatalf("filtered browse visited %d documents, want the budget %d",
			visits, documentScanBudget+1)
	}
}

func TestBrowseDocumentsToleratesScanError(t *testing.T) {
	source := newDocumentBrowseSource(fakeStoredDocuments{
		docs: []documentstore.Document{{CanonicalURL: "https://a.example/1", IndexedAt: day(1)}},
		err:  errors.New("scan failed"),
	})

	page := source.BrowseDocuments(context.Background(), adminui.DocumentQuery{})
	if page.Matched != 1 || len(page.Documents) != 1 {
		t.Fatalf("a scan error should still return what was collected: %+v", page)
	}
}

func TestDocumentSummaryFallsBackToNormalizedURL(t *testing.T) {
	summary := documentSummary(documentstore.Document{NormalizedURL: "https://n.example/1"})
	if summary.URL != "https://n.example/1" {
		t.Fatalf("URL should fall back to the normalized URL, got %q", summary.URL)
	}
	if summary.IndexedAt != "" || summary.FetchedAt != "" {
		t.Fatalf("zero times should render empty, got %+v", summary)
	}
}

func TestDocumentHostHandlesMalformedURL(t *testing.T) {
	if host := documentHost("http://ex\x7fample.com/"); host != "" {
		t.Fatalf("a URL with a control character should have no host, got %q", host)
	}
}
