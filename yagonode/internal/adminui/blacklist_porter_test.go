package adminui

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type fakePorterBlacklist struct {
	entries  []BlacklistEntry
	blocked  map[string]bool
	imported string
}

func (f *fakePorterBlacklist) BlacklistEntries(
	context.Context,
) []BlacklistEntry {
	return f.entries
}

func (f *fakePorterBlacklist) AddBlacklist(context.Context, string, string) error {
	return nil
}

func (f *fakePorterBlacklist) RemoveBlacklist(context.Context, string, string) error {
	return nil
}

func (f *fakePorterBlacklist) BlacklistBlocks(_ context.Context, rawURL string) (bool, error) {
	return f.blocked[rawURL], nil
}

func (f *fakePorterBlacklist) ExportBlacklist(context.Context) (string, error) {
	return "domain spam.example\n", nil
}

func (f *fakePorterBlacklist) ImportBlacklist(_ context.Context, payload string) (int, error) {
	f.imported = payload

	return 2, nil
}

// TestBlacklistProbeExportImport pins UI-17's console surface: the probe
// renders its verdict on the Index page, export streams importable
// plaintext with an attachment header, and import reports the added count.
func TestBlacklistProbeExportImport(t *testing.T) {
	source := &fakePorterBlacklist{blocked: map[string]bool{"https://spam.example/": true}}
	console := New(Options{Index: fakeIndex{snap: IndexStats{Available: true}}, Blacklist: source})

	probe := httptest.NewRecorder()
	console.ServeHTTP(probe, httptest.NewRequestWithContext(
		t.Context(), http.MethodGet,
		"/admin/index/blacklist/test?url="+url.QueryEscape("https://spam.example/"), nil,
	))
	if probe.Code != http.StatusOK || !strings.Contains(probe.Body.String(), "is BLOCKED") {
		t.Fatalf("probe = %d %.120q", probe.Code, probe.Body.String())
	}

	clean := httptest.NewRecorder()
	console.ServeHTTP(clean, httptest.NewRequestWithContext(
		t.Context(), http.MethodGet,
		"/admin/index/blacklist/test?url="+url.QueryEscape("https://ok.example/"), nil,
	))
	if !strings.Contains(clean.Body.String(), "is not blocked") {
		t.Fatal("clean probe verdict missing")
	}

	export := httptest.NewRecorder()
	console.ServeHTTP(export, httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/admin/index/blacklist/export", nil,
	))
	if export.Body.String() != "domain spam.example\n" ||
		!strings.Contains(export.Header().Get("Content-Disposition"), "denylist.txt") {
		t.Fatalf("export = %q %q", export.Body.String(), export.Header().Get("Content-Disposition"))
	}

	form := url.Values{"payload": {"domain a.example\nb.example"}}
	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, "/admin/index/blacklist/import",
		strings.NewReader(form.Encode()),
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	imported := httptest.NewRecorder()
	console.ServeHTTP(imported, request)
	if !strings.Contains(imported.Body.String(), "Imported 2 entries.") ||
		source.imported != "domain a.example\nb.example" {
		t.Fatalf("import note missing: %.150q", imported.Body.String())
	}
}

func TestBlacklistPorterRoutesRequireSource(t *testing.T) {
	console := New(Options{Index: fakeIndex{snap: IndexStats{Available: true}}})
	for _, path := range []string{
		"/admin/index/blacklist/test?url=x",
		"/admin/index/blacklist/export",
	} {
		rec := httptest.NewRecorder()
		console.ServeHTTP(rec, httptest.NewRequestWithContext(
			t.Context(), http.MethodGet, path, nil,
		))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s = %d, want 404 without a source", path, rec.Code)
		}
	}
}

type fakeIndexExporter struct{ req IndexExportRequest }

func (f *fakeIndexExporter) ExportDocuments(
	_ context.Context,
	req IndexExportRequest,
	w io.Writer,
) error {
	f.req = req
	_, err := io.WriteString(w, "https://a.example/\n")

	return err //nolint:wrapcheck // test double passes the writer error through.
}

// TestIndexExportRoute pins UI-18's console surface: filters flow from the
// query string, headers announce a download, unknown formats 400, and the
// route 404s without an exporter.
func TestIndexExportRoute(t *testing.T) {
	exporter := &fakeIndexExporter{}
	console := New(Options{
		Index:       fakeIndex{snap: IndexStats{Available: true}},
		IndexExport: exporter,
	})

	rec := httptest.NewRecorder()
	console.ServeHTTP(rec, httptest.NewRequestWithContext(
		t.Context(), http.MethodGet,
		"/admin/index/export?format=csv&domain=a.example&q=page", nil,
	))
	if rec.Code != http.StatusOK ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), "index-export.csv") ||
		exporter.req != (IndexExportRequest{Format: "csv", Domain: "a.example", URLContains: "page"}) {
		t.Fatalf(
			"export = %d %q %+v",
			rec.Code,
			rec.Header().Get("Content-Disposition"),
			exporter.req,
		)
	}

	bad := httptest.NewRecorder()
	console.ServeHTTP(bad, httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/admin/index/export?format=xml", nil,
	))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("unknown format = %d, want 400", bad.Code)
	}

	none := New(Options{Index: fakeIndex{snap: IndexStats{Available: true}}})
	missing := httptest.NewRecorder()
	none.ServeHTTP(missing, httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/admin/index/export", nil,
	))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("no exporter = %d, want 404", missing.Code)
	}
}
