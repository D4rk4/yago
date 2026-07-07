package adminui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type autocrawlerFakeSettings struct {
	items  []SettingItem
	got    SettingsChange
	result SettingsResult
}

func (f *autocrawlerFakeSettings) Settings(context.Context) SettingsView {
	return SettingsView{Items: f.items}
}

func (f *autocrawlerFakeSettings) Update(
	_ context.Context,
	change SettingsChange,
) (SettingsResult, error) {
	f.got = change

	return f.result, nil
}

func autocrawlerTestSettings() *autocrawlerFakeSettings {
	return &autocrawlerFakeSettings{
		items: []SettingItem{
			{Key: "swarm.seed.enabled", Title: "Greedy learning"},
			{Key: "swarm.seed.depth", Title: "Autocrawler crawl depth", Value: "1"},
			{Key: "web.fallback.seed_crawl", Title: "Web-discovery crawling"},
			{Key: "peer.name", Title: "Peer name"},
		},
		result: SettingsResult{OK: true, Message: "Saved."},
	}
}

// TestAutocrawlerSectionRendersItsSubsetBetweenSearchAndCrawler is the UI-14
// acceptance: the section lives in the nav between Search and Crawler and
// shows only the autocrawler settings, not the whole catalog.
func TestAutocrawlerSectionRendersItsSubsetBetweenSearchAndCrawler(t *testing.T) {
	t.Parallel()

	console := New(Options{Settings: autocrawlerTestSettings()})
	got := do(t, console, "/admin/autocrawler")
	if got.status != http.StatusOK {
		t.Fatalf("status = %d", got.status)
	}
	for _, want := range []string{
		"Autocrawler", "swarm.seed.enabled", "web.fallback.seed_crawl",
		`action="/admin/autocrawler"`,
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("autocrawler page missing %q", want)
		}
	}
	if strings.Contains(got.body, `name="key" value="peer.name"`) {
		t.Fatal("foreign setting leaked into the autocrawler section")
	}
	search := strings.Index(got.body, `cds-nav__label">Search</span>`)
	auto := strings.Index(got.body, `cds-nav__label">Autocrawler</span>`)
	crawler := strings.Index(got.body, `cds-nav__label">Crawler</span>`)
	if search < 0 || search >= auto || auto >= crawler {
		t.Fatalf("nav order wrong: search@%d autocrawler@%d crawler@%d", search, auto, crawler)
	}
}

func TestAutocrawlerUpdateAcceptsOwnKeysOnly(t *testing.T) {
	t.Parallel()

	settings := autocrawlerTestSettings()
	console := New(Options{Settings: settings})

	posted := doPost(t, console, "/admin/autocrawler", url.Values{
		"key": {"swarm.seed.depth"}, "value": {"3"},
	})
	if posted.status != http.StatusOK || !strings.Contains(posted.body, "Saved.") {
		t.Fatalf("update = %d %.60q", posted.status, posted.body)
	}
	if settings.got.Key != "swarm.seed.depth" || settings.got.Value != "3" {
		t.Fatalf("change = %+v", settings.got)
	}

	foreign := doPost(t, console, "/admin/autocrawler", url.Values{
		"key": {"peer.name"}, "value": {"sneaky"},
	})
	if foreign.status != http.StatusNotFound {
		t.Fatalf("foreign key = %d, want 404", foreign.status)
	}
}

func TestAutocrawlerUpdateAcceptsCrawlOptionKeys(t *testing.T) {
	t.Parallel()

	settings := autocrawlerTestSettings()
	console := New(Options{Settings: settings})
	for _, key := range []string{
		"autocrawler.crawl.query_urls",
		"autocrawler.crawl.tls_insecure",
		"autocrawler.crawl.ignore_robots",
		"autocrawler.crawl.no_browser",
		"autocrawler.crawl.follow_nofollow",
	} {
		posted := doPost(t, console, "/admin/autocrawler", url.Values{
			"key": {key}, "value": {"true"},
		})
		if posted.status == http.StatusNotFound {
			t.Fatalf("%s rejected as a foreign key", key)
		}
		if settings.got.Key != key {
			t.Fatalf("change key = %q, want %q", settings.got.Key, key)
		}
	}
}

func TestAutocrawlerRendersFormatToggles(t *testing.T) {
	t.Parallel()

	formats := &fakeFormats{current: FormatSettings{Text: true, PDF: true}}
	console := New(Options{Settings: autocrawlerTestSettings(), CrawlFormats: formats})
	got := do(t, console, "/admin/autocrawler")
	for _, want := range []string{
		"Document formats", `name="text" checked`, `name="pdf" checked`,
		`action="/admin/autocrawler/formats"`, `name="archives"`,
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("autocrawler page missing %q", want)
		}
	}
	if strings.Contains(got.body, `name="archives" checked`) {
		t.Fatal("archives rendered checked while off")
	}
}

func TestAutocrawlerSavesFormatToggles(t *testing.T) {
	t.Parallel()

	formats := &fakeFormats{}
	console := New(Options{Settings: autocrawlerTestSettings(), CrawlFormats: formats})
	got := doPost(t, console, "/admin/autocrawler/formats", url.Values{
		"text": {"on"}, "images": {"on"},
	})
	if got.status != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", got.status)
	}
	if loc := got.header.Get("Location"); loc != autocrawlerPath {
		t.Fatalf("redirect = %q, want %q", loc, autocrawlerPath)
	}
	if formats.saved == nil || !formats.saved.Text || !formats.saved.Images ||
		formats.saved.PDF || formats.saved.Archives {
		t.Fatalf("saved = %+v", formats.saved)
	}

	bare := New(Options{Settings: autocrawlerTestSettings()})
	if got := doPost(
		t,
		bare,
		"/admin/autocrawler/formats",
		url.Values{},
	); got.status != http.StatusNotFound {
		t.Fatalf("formats save without source = %d, want 404", got.status)
	}
}

func TestAutocrawlerFormatsErrorPaths(t *testing.T) {
	t.Parallel()

	failing := &fakeFormats{err: context.DeadlineExceeded}
	console := New(Options{Settings: autocrawlerTestSettings(), CrawlFormats: failing})
	got := doPost(t, console, "/admin/autocrawler/formats", url.Values{"text": {"on"}})
	if got.status != http.StatusOK ||
		!strings.Contains(got.body, "Saving format settings failed.") {
		t.Fatalf("save failure = %d, want rendered note", got.status)
	}

	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/admin/autocrawler/formats",
		strings.NewReader("%zz"),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	console.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed body = %d, want 400", rec.Code)
	}
}

func TestAutocrawlerUnavailableWithoutSettings(t *testing.T) {
	t.Parallel()

	console := New(Options{})
	got := do(t, console, "/admin/autocrawler")
	if got.status != http.StatusOK || !strings.Contains(got.body, "not available") {
		t.Fatalf("unavailable page = %d", got.status)
	}
	posted := doPost(t, console, "/admin/autocrawler", url.Values{"key": {"swarm.seed.enabled"}})
	if posted.status != http.StatusNotFound {
		t.Fatalf("update without settings = %d, want 404", posted.status)
	}
}
