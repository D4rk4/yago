package adminui

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

type fakeTerms struct {
	report TermReport
	last   string
}

func (f *fakeTerms) LookupTerm(_ context.Context, term string) TermReport {
	f.last = term

	return f.report
}

func indexSchemaFixture() []SchemaGroup {
	return []SchemaGroup{{
		Title:  "Full-text search index",
		Fields: []SchemaField{{Name: "body", Description: "Main extracted body text."}},
	}}
}

func TestConsoleIndexRendersTermBrowserAndSchema(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Index:  fakeIndex{snap: IndexStats{Available: true, Documents: 3}},
		Terms:  &fakeTerms{},
		Schema: indexSchemaFixture(),
	})
	got := do(t, console, "/admin/index")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{
		"Term browser", `name="term"`, "Index schema",
		"Full-text search index", "body", "Main extracted body text.",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("index page missing %q", want)
		}
	}
}

func TestConsoleIndexTermLookupShowsResults(t *testing.T) {
	t.Parallel()

	terms := &fakeTerms{report: TermReport{
		Term:  "golang",
		Hash:  "WWWWWWWWWWWW",
		Count: 2,
		Sample: []TermPosting{
			{URL: "http://a.example/1", Title: "Alpha"},
		},
	}}
	console := New(Options{
		Index: fakeIndex{snap: IndexStats{Available: true}},
		Terms: terms,
	})

	got := do(t, console, "/admin/index?term=golang")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if terms.last != "golang" {
		t.Fatalf("term not passed to source: %q", terms.last)
	}
	for _, want := range []string{"2 posting(s)", "WWWWWWWWWWWW", "http://a.example/1", "Alpha"} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("term results missing %q", want)
		}
	}
}

func TestConsoleIndexTermNotFound(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Index: fakeIndex{snap: IndexStats{Available: true}},
		Terms: &fakeTerms{report: TermReport{Term: "absent", Hash: "H", NotFound: true}},
	})
	got := do(t, console, "/admin/index?term=absent")
	if !strings.Contains(got.body, "No postings for") {
		t.Fatal("expected the not-found message")
	}
}

func TestConsoleIndexOmitsTermBrowserWithoutSource(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{Index: fakeIndex{snap: IndexStats{Available: true}}}), "/admin/index")
	if strings.Contains(got.body, "Term browser") {
		t.Fatal("term browser rendered without a term source")
	}
}

type fakeDocuments struct {
	page DocumentPage
	got  DocumentQuery
}

func (f *fakeDocuments) BrowseDocuments(_ context.Context, query DocumentQuery) DocumentPage {
	f.got = query

	return f.page
}

func TestConsoleIndexRendersDocumentBrowser(t *testing.T) {
	t.Parallel()

	docs := &fakeDocuments{page: DocumentPage{
		Documents: []DocumentSummary{{
			URL: "https://a.example/1", Title: "Doc One",
			ContentType: "text/html", Language: "en", IndexedAt: "2026-07-04T00:00:00Z",
		}},
		Matched: 60, Limit: 50, Truncated: true,
	}}
	console := New(Options{
		Index:     fakeIndex{snap: IndexStats{Available: true}},
		Documents: docs,
	})

	got := do(t, console, "/admin/index?q=example&domain=a.example")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if docs.got.URLContains != "example" || docs.got.Domain != "a.example" {
		t.Fatalf("filters not passed to source: %+v", docs.got)
	}
	for _, want := range []string{
		"Document browser", "https://a.example/1", "Doc One", "text/html",
		"60 matching document(s)", "showing the first 50",
		`value="example"`, `value="a.example"`,
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("document browser missing %q", want)
		}
	}
}

func TestConsoleIndexDocumentBrowserEmptyState(t *testing.T) {
	t.Parallel()

	got := do(
		t,
		New(
			Options{
				Index:     fakeIndex{snap: IndexStats{Available: true}},
				Documents: &fakeDocuments{},
			},
		),
		"/admin/index",
	)
	if !strings.Contains(got.body, "No documents match.") {
		t.Fatal("expected the empty document-browser state")
	}
}

func TestConsoleIndexOmitsDocumentBrowserWithoutSource(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{Index: fakeIndex{snap: IndexStats{Available: true}}}), "/admin/index")
	if strings.Contains(got.body, "Document browser") {
		t.Fatal("document browser rendered without a source")
	}
}
