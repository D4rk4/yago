package adminui

import (
	"strings"
	"testing"
)

func TestConfigRelocatesAutomaticDiscoveryKeysIntoOneCrawlerFieldset(t *testing.T) {
	view := SettingsView{Items: []SettingItem{
		{Key: "swarm.seed.depth", Title: "Autocrawler crawl depth", Category: "Swarm"},
		{Key: "swarm.morphology.enabled", Title: "Swarm morphology", Category: "Swarm"},
		{Key: "web.fallback.seed_crawl", Title: "Web-discovery crawling", Category: "Web fallback"},
		{Key: "web.fallback.timeout", Title: "Web fallback timeout", Category: "Web fallback"},
		{Key: "autocrawler.crawl.ignore_robots", Title: "Ignore robots", Category: "Crawler"},
		{Key: "crawl.ingest.quality_gate", Title: "Ingest quality gate", Category: "Crawler"},
	}}
	body := do(t, New(Options{
		Config: fakeConfig{}, Settings: &fakeSettings{view: view},
	}), configPath).body
	for _, key := range []string{
		"swarm.seed.depth", "web.fallback.seed_crawl", "autocrawler.crawl.ignore_robots",
	} {
		if count := strings.Count(body, `name="key" value="`+key+`"`); count != 1 {
			t.Fatalf("automatic setting %q rendered %d times, want once", key, count)
		}
	}
	crawlerStart := strings.Index(body, `id="panel-crawler"`)
	if crawlerStart < 0 {
		t.Fatal("Crawler panel is missing")
	}
	crawlerEnd := strings.Index(body[crawlerStart:], `</section>`)
	if crawlerEnd < 0 {
		t.Fatal("Crawler panel does not close")
	}
	crawler := body[crawlerStart : crawlerStart+crawlerEnd]
	for _, want := range []string{
		">Automatic discovery</legend>", "swarm.seed.depth",
		"web.fallback.seed_crawl", "autocrawler.crawl.ignore_robots",
		">Crawler</legend>", "crawl.ingest.quality_gate",
	} {
		if !strings.Contains(crawler, want) {
			t.Fatalf("Crawler panel missing %q", want)
		}
	}
	for _, kept := range []string{"swarm.morphology.enabled", "web.fallback.timeout"} {
		if !strings.Contains(body, kept) {
			t.Fatalf("Configuration dropped genuine setting %q", kept)
		}
	}
}
