package cachedpage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type fakeDirectory struct {
	doc   documentstore.Document
	found bool
	err   error
}

func (f fakeDirectory) Document(
	context.Context,
	string,
) (documentstore.Document, bool, error) {
	return f.doc, f.found, f.err
}

func (f fakeDirectory) Count(context.Context) (int, error) { return 0, nil }

func get(
	t *testing.T,
	directory documentstore.DocumentDirectory,
	target string,
) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	Mount(mux, directory)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil))

	return rec
}

func TestCachedCopyRendersStoredDocumentEscaped(t *testing.T) {
	directory := fakeDirectory{found: true, doc: documentstore.Document{
		NormalizedURL: "https://example.org/a",
		Title:         "Stored <b>Page</b>",
		ExtractedText: "First paragraph.\n\nSecond <script>alert(1)</script> paragraph.\n\n  ",
		FetchedAt:     time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
	}}

	rec := get(t, directory, URLFor("https://example.org/a"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Cached copy", "Stored &lt;b&gt;Page&lt;/b&gt;", "https://example.org/a",
		"2026-07-01T12:00:00Z", "First paragraph.",
		"Second &lt;script&gt;alert(1)&lt;/script&gt; paragraph.",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("cached page missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatal("stored text rendered unescaped")
	}
}

func TestCachedCopyFallsBackToURLTitleWithoutFetchTime(t *testing.T) {
	directory := fakeDirectory{found: true, doc: documentstore.Document{
		NormalizedURL: "https://example.org/bare",
	}}

	rec := get(t, directory, URLFor("https://example.org/bare"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<h1>https://example.org/bare</h1>") {
		t.Fatalf("missing URL title fallback: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), " on <") {
		t.Fatal("fetch time rendered for a document without one")
	}
}

func TestCachedCopyErrorPaths(t *testing.T) {
	if rec := get(t, fakeDirectory{found: true}, Path); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing u: status = %d, want 400", rec.Code)
	}
	if rec := get(
		t,
		fakeDirectory{},
		URLFor("https://x.example/"),
	); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown url: status = %d, want 404", rec.Code)
	}
	rec := get(t, fakeDirectory{err: errors.New("boom")}, URLFor("https://x.example/"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("lookup error: status = %d, want 500", rec.Code)
	}
}

type failingWriter struct{ header http.Header }

func (w failingWriter) Header() http.Header       { return w.header }
func (w failingWriter) WriteHeader(int)           {}
func (w failingWriter) Write([]byte) (int, error) { return 0, errors.New("client gone") }

func TestCachedCopyToleratesRenderFailure(t *testing.T) {
	directory := fakeDirectory{found: true, doc: documentstore.Document{
		NormalizedURL: "https://example.org/a",
	}}
	req := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, URLFor("https://example.org/a"), nil,
	)

	endpoint{documents: directory}.ServeHTTP(failingWriter{header: http.Header{}}, req)
}

func TestMountSkipsNilDirectory(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, URLFor("https://x.example/"), nil,
	))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("nil directory route status = %d, want 404", rec.Code)
	}
}
