package adminui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func automaticDiscoverySettingsView() SettingsView {
	return SettingsView{Items: []SettingItem{
		{
			Key: "swarm.seed.enabled", Title: "Greedy learning", Value: "true",
			Boolean: true, Category: "Swarm",
		},
		{Key: "swarm.seed.depth", Title: "Autocrawler crawl depth", Value: "5", Category: "Swarm"},
		{
			Key: "web.fallback.seed_crawl", Title: "Web-discovery crawling", Value: "false",
			Boolean: true, Category: "Web fallback",
		},
		{
			Key: "autocrawler.crawl.no_browser", Title: "Disable browser rendering",
			Value: "true", Boolean: true, Category: "Crawler",
		},
		{
			Key:   "crawler.prioritize_automatic_discovery",
			Title: "Prioritize automatic discovery crawls", Value: "true",
			Boolean: true, Category: "Crawler",
		},
		{
			Key: "crawler.fetch_workers", Title: "Maximum fetch concurrency per crawler",
			Value: "4", Category: "Crawler",
		},
		{
			Key: "crawler.max_pages_per_second", Title: "Maximum fleet-wide fetch-start rate",
			Value: "10", Category: "Crawler",
		},
		{
			Key: "crawler.max_redirects", Title: "Maximum redirects per page",
			Value: "10", Category: "Crawler",
		},
		{
			Key: "crawler.max_active_runs", Title: "Maximum active crawl tasks",
			Value: "32", Category: "Crawler",
		},
	}}
}

func TestLegacyAutocrawlerRoutesRedirectToConfigurationCrawler(t *testing.T) {
	console := New(Options{})
	get := do(t, console, autocrawlerPath)
	if get.status != http.StatusPermanentRedirect ||
		get.header.Get("Location") != autocrawlerConfigurationLocation {
		t.Fatalf("GET redirect = %d %q", get.status, get.header.Get("Location"))
	}
	for _, path := range []string{autocrawlerPath, autocrawlerPath + "/formats"} {
		posted := doPost(t, console, path, url.Values{"text": {"on"}})
		if posted.status != http.StatusSeeOther ||
			posted.header.Get("Location") != autocrawlerConfigurationLocation {
			t.Fatalf("POST %s redirect = %d %q", path, posted.status,
				posted.header.Get("Location"))
		}
	}
}

func TestConfigurationCrawlerOwnsAutomaticDiscoveryAndFormats(t *testing.T) {
	formats := &fakeFormats{current: FormatSettings{Text: true, PDF: true}}
	console := New(Options{
		Config:       fakeConfig{view: ConfigView{}},
		Settings:     &fakeSettings{view: automaticDiscoverySettingsView()},
		CrawlFormats: formats,
	})
	got := do(t, console, configPath)
	for _, want := range []string{
		`id="panel-crawler"`, `>Automatic discovery</legend>`,
		`name="value:swarm.seed.enabled"`, `name="value:web.fallback.seed_crawl"`,
		`name="value:crawler.prioritize_automatic_discovery"`,
		`>Crawler</legend>`, `name="value:crawler.fetch_workers"`,
		`name="value:crawler.max_pages_per_second"`,
		`name="value:crawler.max_redirects"`,
		`name="value:crawler.max_active_runs"`,
		`>Document formats</legend>`, `action="/admin/configuration/formats#panel-crawler"`,
		`name="text" checked`, `name="pdf" checked`, `name="archives"`,
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("Configuration Crawler missing %q in %.1200s", want, got.body)
		}
	}
	if strings.Contains(got.body, `cds-nav__label">Autocrawler</span>`) {
		t.Fatal("obsolete Autocrawler navigation entry is still rendered")
	}
}

func TestConfigurationCrawlerSavesMaximumRedirectsPerPage(t *testing.T) {
	settings := &fakeSettings{
		view:   automaticDiscoverySettingsView(),
		result: SettingsResult{OK: true, Message: "Saved."},
	}
	console := New(Options{Config: fakeConfig{}, Settings: settings})
	got := doPost(t, console, configPath, url.Values{
		"key":                         {"crawler.max_redirects"},
		"value:crawler.max_redirects": {"7"},
	})
	if got.status != http.StatusOK || len(settings.changes) != 1 {
		t.Fatalf("save = status %d changes %+v", got.status, settings.changes)
	}
	if change := settings.changes[0]; change.Key != "crawler.max_redirects" ||
		change.Value != "7" {
		t.Fatalf("saved change = %+v", change)
	}
}

func TestConfigurationCrawlerSavesMaximumFleetWideFetchStartRate(t *testing.T) {
	settings := &fakeSettings{
		view:   automaticDiscoverySettingsView(),
		result: SettingsResult{OK: true, Message: "Saved."},
	}
	console := New(Options{Config: fakeConfig{}, Settings: settings})
	got := doPost(t, console, configPath, url.Values{
		"key":                                {"crawler.max_pages_per_second"},
		"value:crawler.max_pages_per_second": {"23"},
	})
	if got.status != http.StatusOK || len(settings.changes) != 1 {
		t.Fatalf("save = status %d changes %+v", got.status, settings.changes)
	}
	if change := settings.changes[0]; change.Key != "crawler.max_pages_per_second" ||
		change.Value != "23" {
		t.Fatalf("saved change = %+v", change)
	}
}

func TestConfigurationCrawlerSavesAutomaticDiscoverySetting(t *testing.T) {
	settings := &fakeSettings{
		view:   automaticDiscoverySettingsView(),
		result: SettingsResult{OK: true, Message: "Saved."},
	}
	console := New(Options{Config: fakeConfig{}, Settings: settings})
	got := doPost(t, console, configPath, url.Values{
		"key": {"crawler.prioritize_automatic_discovery"},
		"bool:crawler.prioritize_automatic_discovery": {"1"},
	})
	if got.status != http.StatusOK || len(settings.changes) != 1 {
		t.Fatalf("save = status %d changes %+v", got.status, settings.changes)
	}
	if change := settings.changes[0]; change.Key != "crawler.prioritize_automatic_discovery" ||
		change.Value != "false" {
		t.Fatalf("saved change = %+v", change)
	}
}

func TestConfigurationCrawlerSavesMaximumActiveTasks(t *testing.T) {
	settings := &fakeSettings{
		view:   automaticDiscoverySettingsView(),
		result: SettingsResult{OK: true, Message: "Saved."},
	}
	console := New(Options{Config: fakeConfig{}, Settings: settings})
	got := doPost(t, console, configPath, url.Values{
		"key":                           {"crawler.max_active_runs"},
		"value:crawler.max_active_runs": {"37"},
	})
	if got.status != http.StatusOK || len(settings.changes) != 1 {
		t.Fatalf("save = status %d changes %+v", got.status, settings.changes)
	}
	if change := settings.changes[0]; change.Key != "crawler.max_active_runs" ||
		change.Value != "37" {
		t.Fatalf("saved change = %+v", change)
	}
}

func TestConfigurationCrawlerSavesFormats(t *testing.T) {
	formats := &fakeFormats{}
	console := New(Options{Config: fakeConfig{}, CrawlFormats: formats})
	got := doPost(t, console, configPath+"/formats", url.Values{
		"text": {"on"}, "images": {"on"}, "archives": {"on"},
	})
	if got.status != http.StatusSeeOther ||
		got.header.Get("Location") != configPath+"?saved=formats#panel-crawler" {
		t.Fatalf("format save redirect = %d %q", got.status, got.header.Get("Location"))
	}
	if formats.saved == nil || !formats.saved.Text || !formats.saved.Images ||
		!formats.saved.Archives || formats.saved.PDF {
		t.Fatalf("saved formats = %+v", formats.saved)
	}
	saved := do(t, console, configPath+"?saved=formats")
	if !strings.Contains(
		saved.body,
		`class="cds-toast cds-toast--success" role="status">Document format settings updated.</div>`,
	) {
		t.Fatal("format save notice is missing")
	}
	if strings.Contains(
		saved.body,
		`cds-toast--error" role="alert">Document format settings updated.`,
	) {
		t.Fatal("format save success is styled as an error")
	}
}

func TestConfigurationCrawlerFormatsRequireBothSources(t *testing.T) {
	tests := []struct {
		name    string
		options Options
	}{
		{name: "configuration only", options: Options{Config: fakeConfig{}}},
		{name: "formats only", options: Options{CrawlFormats: &fakeFormats{}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := doPost(t, New(test.options), configPath+"/formats", url.Values{
				"text": {"on"},
			})
			if got.status != http.StatusNotFound {
				t.Fatalf("format save status = %d, want 404", got.status)
			}
		})
	}
}

func TestConfigurationCrawlerFormatErrorStates(t *testing.T) {
	loadFailure := New(Options{
		Config:       fakeConfig{},
		Settings:     &fakeSettings{view: automaticDiscoverySettingsView()},
		CrawlFormats: &fakeFormats{readErr: context.Canceled},
	})
	if got := do(t, loadFailure, configPath); !strings.Contains(
		got.body,
		"Document format settings are unavailable.",
	) {
		t.Fatal("format load error is missing")
	}

	saveFailure := New(Options{
		Config:       fakeConfig{},
		Settings:     &fakeSettings{view: automaticDiscoverySettingsView()},
		CrawlFormats: &fakeFormats{err: context.Canceled},
	})
	got := doPost(t, saveFailure, configPath+"/formats", url.Values{"text": {"on"}})
	if got.status != http.StatusOK ||
		!strings.Contains(got.body, "Saving format settings failed.") {
		t.Fatalf("format save failure = %d", got.status)
	}
	if !strings.Contains(
		got.body,
		`class="cds-inline-notification cds-inline-notification--error" role="alert">Saving format settings failed.</div>`,
	) {
		t.Fatal("format save failure is not styled as an error")
	}

	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		configPath+"/formats",
		strings.NewReader("%zz"),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	saveFailure.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("malformed format form = %d, want 400", recorder.Code)
	}
}
