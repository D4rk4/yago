package adminui

import (
	"strings"
	"testing"
)

// The Autocrawler page owns the greedy-learning and web-seeding settings; the
// flat Configuration sheet must not duplicate them, while genuine swarm and
// web-fallback settings still surface there.
func TestConfigExcludesAutocrawlerOwnedKeys(t *testing.T) {
	t.Parallel()

	view := SettingsView{Items: []SettingItem{
		{Key: "swarm.seed.depth", Title: "Autocrawler crawl depth", Category: "Swarm"},
		{Key: "swarm.seed.max_pages", Title: "Autocrawler pages per host", Category: "Swarm"},
		{
			Key:      "swarm.morphology.enabled",
			Title:    "Swarm morphology",
			Category: "Swarm",
			Boolean:  true,
		},
		{
			Key:      "web.fallback.seed_crawl",
			Title:    "Web-discovery seeding",
			Category: "Web fallback",
			Boolean:  true,
		},
		{Key: "web.fallback.timeout", Title: "Web fallback timeout", Category: "Web fallback"},
		{
			Key:      "autocrawler.crawl.ignore_robots",
			Title:    "Ignore robots",
			Category: "Crawler",
			Boolean:  true,
		},
		{
			Key:      "crawl.ingest.quality_gate",
			Title:    "Ingest quality gate",
			Category: "Crawler",
			Boolean:  true,
		},
	}}
	console := New(
		Options{Config: fakeConfig{view: ConfigView{}}, Settings: &fakeSettings{view: view}},
	)

	body := do(t, console, configPath).body
	for _, gone := range []string{
		"swarm.seed.depth", "Autocrawler crawl depth",
		"swarm.seed.max_pages", "Autocrawler pages per host",
		"web.fallback.seed_crawl", "autocrawler.crawl.ignore_robots",
	} {
		if strings.Contains(body, gone) {
			t.Fatalf("Configuration must not duplicate autocrawler-owned key %q", gone)
		}
	}
	for _, kept := range []string{
		"swarm.morphology.enabled", "Swarm morphology",
		"web.fallback.timeout", "crawl.ingest.quality_gate",
	} {
		if !strings.Contains(body, kept) {
			t.Fatalf("Configuration dropped a genuine setting %q", kept)
		}
	}
}
