package yagonode

import (
	"testing"
	"time"
)

// TestAutocrawlerCrawlOptionCatalog covers the five per-crawl toggles the
// autocrawler page exposes: their defaults, boolean normalization, and that
// apply writes the matching AutocrawlerCrawl field.
func TestAutocrawlerCrawlOptionCatalog(t *testing.T) {
	t.Parallel()

	base := nodeConfig{AutocrawlerCrawl: defaultSeedCrawlOptions()}
	byKey := indexSettingDefinitions()

	cases := []struct {
		key          string
		defaultValue string
		read         func(config nodeConfig) bool
	}{
		{"autocrawler.crawl.query_urls", "true", func(c nodeConfig) bool {
			return c.AutocrawlerCrawl.AllowQueryURLs
		}},
		{"autocrawler.crawl.tls_insecure", "true", func(c nodeConfig) bool {
			return c.AutocrawlerCrawl.IgnoreTLSAuthority
		}},
		{"autocrawler.crawl.ignore_robots", "false", func(c nodeConfig) bool {
			return c.AutocrawlerCrawl.IgnoreRobots
		}},
		{"autocrawler.crawl.no_browser", "false", func(c nodeConfig) bool {
			return c.AutocrawlerCrawl.DisableBrowser
		}},
		{"autocrawler.crawl.follow_nofollow", "false", func(c nodeConfig) bool {
			return c.AutocrawlerCrawl.FollowNoFollowLinks
		}},
	}

	for _, tc := range cases {
		def, ok := byKey[tc.key]
		if !ok {
			t.Fatalf("setting %q missing from the catalog", tc.key)
		}
		if got := def.defaultValue(base); got != tc.defaultValue {
			t.Fatalf("%s default = %q, want %q", tc.key, got, tc.defaultValue)
		}
		if _, err := def.normalize("nonsense"); err == nil {
			t.Fatalf("%s must reject a non-boolean value", tc.key)
		}
		on, err := def.normalize("true")
		if err != nil || on != settingBoolTrue {
			t.Fatalf("%s normalize(true) = %q %v", tc.key, on, err)
		}
		if enabled := def.apply(base, on); !tc.read(enabled) {
			t.Fatalf("%s apply(true) did not set its field", tc.key)
		}
		off, err := def.normalize("false")
		if err != nil {
			t.Fatalf("%s normalize(false): %v", tc.key, err)
		}
		if disabled := def.apply(base, off); tc.read(disabled) {
			t.Fatalf("%s apply(false) left its field set", tc.key)
		}
	}
}

// TestAutocrawlerRecrawlIntervalSetting covers the recrawl-cadence setting: the
// default renders as the shipped 30d, a friendly value is canonicalized, "off"
// disables recrawling, garbage is rejected, and apply writes the config field.
func TestAutocrawlerRecrawlIntervalSetting(t *testing.T) {
	t.Parallel()

	base := nodeConfig{AutocrawlerCrawl: defaultSeedCrawlOptions()}
	def, ok := indexSettingDefinitions()["autocrawler.crawl.recrawl_interval"]
	if !ok {
		t.Fatal("autocrawler.crawl.recrawl_interval missing from the catalog")
	}
	if got := def.defaultValue(base); got != "30d" {
		t.Fatalf("default = %q, want 30d", got)
	}
	canonical, err := def.normalize("720h")
	if err != nil || canonical != "30d" {
		t.Fatalf("normalize(720h) = %q %v, want 30d", canonical, err)
	}
	off, err := def.normalize("off")
	if err != nil || off != "off" {
		t.Fatalf("normalize(off) = %q %v, want off", off, err)
	}
	if _, err := def.normalize("nonsense"); err == nil {
		t.Fatal("normalize must reject an invalid interval")
	}
	if applied := def.apply(
		base,
		canonical,
	); applied.AutocrawlerCrawl.RecrawlInterval != 30*24*time.Hour {
		t.Fatalf("apply(30d) = %v, want 720h", applied.AutocrawlerCrawl.RecrawlInterval)
	}
	if disabled := def.apply(base, off); disabled.AutocrawlerCrawl.RecrawlInterval != 0 {
		t.Fatalf("apply(off) = %v, want 0", disabled.AutocrawlerCrawl.RecrawlInterval)
	}
}
