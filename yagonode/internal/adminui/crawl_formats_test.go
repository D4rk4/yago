package adminui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type fakeFormats struct {
	current FormatSettings
	saved   *FormatSettings
	err     error
	readErr error
}

func (f *fakeFormats) CurrentFormats(context.Context) (FormatSettings, error) {
	return f.current, f.readErr
}

func (f *fakeFormats) SaveFormats(_ context.Context, settings FormatSettings) error {
	f.saved = &settings

	return f.err
}

func TestConsoleCrawlRendersFormatToggles(t *testing.T) {
	t.Parallel()

	formats := &fakeFormats{current: FormatSettings{Text: true, PDF: true}}
	console := New(Options{Crawl: &fakeCrawl{}, CrawlFormats: formats})
	got := do(t, console, "/admin/crawl")
	for _, want := range []string{
		"Document formats", `name="text" checked`, `name="pdf" checked`,
		`name="archives"`, "security risk",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("crawl page missing %q", want)
		}
	}
	if strings.Contains(got.body, `name="archives" checked`) {
		t.Fatal("archives rendered checked while off")
	}
}

func TestConsoleCrawlReportsUnavailableFormatToggles(t *testing.T) {
	t.Parallel()

	formats := &fakeFormats{readErr: context.Canceled}
	got := do(t, New(Options{Crawl: &fakeCrawl{}, CrawlFormats: formats}), "/admin/crawl")
	if !strings.Contains(got.body, "Document format settings are unavailable.") {
		t.Fatalf("missing unavailable format state: %.120q", got.body)
	}
	if strings.Contains(got.body, `name="text"`) {
		t.Fatal("unavailable format settings rendered invented toggles")
	}
}

func TestConsoleCrawlSavesFormatToggles(t *testing.T) {
	t.Parallel()

	formats := &fakeFormats{}
	console := New(Options{Crawl: &fakeCrawl{}, CrawlFormats: formats})
	got := doPost(t, console, "/admin/crawl/formats", url.Values{
		"text": {"on"}, "images": {"on"}, "archives": {"on"},
	})
	if got.status != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", got.status)
	}
	if formats.saved == nil || !formats.saved.Text || !formats.saved.Images ||
		!formats.saved.Archives || formats.saved.PDF {
		t.Fatalf("saved = %+v", formats.saved)
	}

	bare := New(Options{Crawl: &fakeCrawl{}})
	if got := doPost(
		t,
		bare,
		"/admin/crawl/formats",
		url.Values{},
	); got.status != http.StatusNotFound {
		t.Fatalf("formats save without source = %d, want 404", got.status)
	}
}

func TestConsoleCrawlFormatsErrorPaths(t *testing.T) {
	t.Parallel()

	failing := &fakeFormats{err: context.DeadlineExceeded}
	console := New(Options{Crawl: &fakeCrawl{}, CrawlFormats: failing})
	got := doPost(t, console, "/admin/crawl/formats", url.Values{"text": {"on"}})
	if got.status != http.StatusOK ||
		!strings.Contains(got.body, "Saving format settings failed.") {
		t.Fatalf("save failure = %d, want rendered note", got.status)
	}

	// A malformed body fails form parsing.
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/admin/crawl/formats",
		strings.NewReader("%zz"),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	console.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed body = %d, want 400", rec.Code)
	}
}
