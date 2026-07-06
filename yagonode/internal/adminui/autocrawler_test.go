package adminui

import (
	"context"
	"net/http"
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
	search := strings.Index(got.body, `>Search</a>`)
	auto := strings.Index(got.body, `>Autocrawler</a>`)
	crawler := strings.Index(got.body, `>Crawler</a>`)
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
