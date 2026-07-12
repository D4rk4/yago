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

func TestCachedCopyBoundsURLBeforeDocumentLookup(t *testing.T) {
	exact := strings.Repeat("x", maximumCachedPageURLBytes)
	if rec := get(t, fakeDirectory{}, URLFor(exact)); rec.Code != http.StatusNotFound {
		t.Fatalf("exact URL status = %d, want 404", rec.Code)
	}
	oversized := strings.Repeat("x", maximumCachedPageURLBytes+1)
	if rec := get(
		t,
		fakeDirectory{err: errors.New("lookup reached")},
		URLFor(oversized),
	); rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized URL status = %d, want 400", rec.Code)
	}
}

func TestCachedCopyAdmissionBoundsLookupAndReleases(t *testing.T) {
	gate := newCachedPageAdmission(1)
	release, admitted := gate.tryAcquire()
	if !admitted {
		t.Fatal("failed to reserve admission fixture")
	}
	e := endpoint{
		documents: fakeDirectory{err: errors.New("boom")},
		admission: gate,
	}
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		URLFor("https://example.org/a"),
		nil,
	)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, request)
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("saturated status = %d retry=%q", rec.Code, rec.Header().Get("Retry-After"))
	}
	release()

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, request)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("lookup error status = %d, want 500", rec.Code)
	}
	finalRelease, admitted := gate.tryAcquire()
	if !admitted {
		t.Fatal("lookup retained admission slot")
	}
	finalRelease()
}

func TestCachedCopyAdmissionAllowsUnlimitedWorkWhenDisabled(t *testing.T) {
	gate := newCachedPageAdmission(0)
	if gate != nil {
		t.Fatal("disabled admission is not nil")
	}
	release, admitted := gate.tryAcquire()
	if !admitted {
		t.Fatal("disabled admission rejected work")
	}
	release()
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
