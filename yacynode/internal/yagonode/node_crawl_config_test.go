package yagonode

import "testing"

func TestLoadCrawlConfigDisabledWhenNoURL(t *testing.T) {
	cfg, err := loadCrawlConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Enabled() {
		t.Fatal("crawl should be disabled without NATS_URL")
	}
}

func TestLoadCrawlConfigDefaults(t *testing.T) {
	env := map[string]string{envNATSURL: "nats://localhost:4222"}
	cfg, err := loadCrawlConfig(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Enabled() {
		t.Fatal("crawl should be enabled")
	}
	if cfg.OrdersSubject != defaultOrdersSubject ||
		cfg.IngestSubject != defaultIngestSubject ||
		cfg.IngestDurable != defaultIngestDurable ||
		cfg.IngestMaxMsgs != defaultIngestMaxMsgs {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadCrawlConfigOverrides(t *testing.T) {
	env := map[string]string{
		envNATSURL:           "nats://localhost:4222",
		envNATSOrdersSubject: "o",
		envNATSIngestSubject: "i",
		envNATSIngestDurable: "d",
		envNATSIngestMaxMsgs: "5",
	}
	cfg, err := loadCrawlConfig(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.OrdersSubject != "o" || cfg.IngestSubject != "i" ||
		cfg.IngestDurable != "d" || cfg.IngestMaxMsgs != 5 {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}

func TestLoadCrawlConfigRejectsBadMaxMsgs(t *testing.T) {
	for _, raw := range []string{"abc", "0", "-1"} {
		env := map[string]string{envNATSURL: "nats://x", envNATSIngestMaxMsgs: raw}
		if _, err := loadCrawlConfig(func(k string) string { return env[k] }); err == nil {
			t.Fatalf("expected error for max msgs %q", raw)
		}
	}
}
