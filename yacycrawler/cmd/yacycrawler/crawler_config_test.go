package main

import (
	"testing"
	"time"
)

func envFrom(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestDefaultCrawlConfig(t *testing.T) {
	cfg := DefaultCrawlConfig()
	if cfg.Workers <= 0 || cfg.JobQueueSize <= 0 || cfg.MaxBodyBytes <= 0 {
		t.Errorf("default config has non-positive bounds: %+v", cfg)
	}
}

func TestLoadServiceConfigRequiresNATSURL(t *testing.T) {
	if _, err := LoadServiceConfig(envFrom(nil)); err == nil {
		t.Fatal("expected error when NATS_URL is unset")
	}
}

func TestLoadServiceConfigRequiresProxyURL(t *testing.T) {
	if _, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNATSURL: "nats://localhost:4222",
	})); err == nil {
		t.Fatal("expected error when YACYCRAWLER_PROXY_URL is unset")
	}
}

func TestLoadServiceConfigRejectsNonHTTPProxyURL(t *testing.T) {
	if _, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNATSURL:  "nats://localhost:4222",
		EnvProxyURL: "socks5://proxy:1080",
	})); err == nil {
		t.Fatal("expected error for non-http proxy scheme")
	}
}

func TestLoadServiceConfigDefaults(t *testing.T) {
	cfg, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNATSURL:  "nats://localhost:4222",
		EnvProxyURL: "http://proxy:4750",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ProxyURL == nil || cfg.ProxyURL.String() != "http://proxy:4750" {
		t.Errorf("proxy url = %v", cfg.ProxyURL)
	}
	if cfg.OrdersSubject != DefaultOrdersSubject {
		t.Errorf("orders subject = %q", cfg.OrdersSubject)
	}
	if cfg.IngestSubject != DefaultIngestSubject {
		t.Errorf("ingest subject = %q", cfg.IngestSubject)
	}
	if cfg.OrdersDurable != DefaultOrdersDurable {
		t.Errorf("durable = %q", cfg.OrdersDurable)
	}
	if cfg.IngestMaxMsgs != DefaultIngestMaxMsgs {
		t.Errorf("max msgs = %d", cfg.IngestMaxMsgs)
	}
	spec := cfg.StreamSpec()
	if spec.OrdersSubject != cfg.OrdersSubject ||
		spec.IngestSubject != cfg.IngestSubject ||
		spec.IngestMaxMsgs != cfg.IngestMaxMsgs {
		t.Errorf("stream spec mismatch: %+v", spec)
	}
}

func TestLoadServiceConfigOverrides(t *testing.T) {
	cfg, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNATSURL:           "nats://localhost:4222",
		EnvProxyURL:          "http://proxy:4750",
		EnvNATSOrdersSubject: "o.subject",
		EnvNATSIngestSubject: "i.subject",
		EnvNATSDurable:       "dur",
		EnvNATSIngestMaxMsgs: "7",
		EnvWorkers:           "3",
		EnvMaxDepth:          "5",
		EnvCrawlDelay:        "250ms",
		EnvUserAgent:         "test-agent",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.OrdersSubject != "o.subject" || cfg.IngestSubject != "i.subject" {
		t.Errorf("subjects = %q %q", cfg.OrdersSubject, cfg.IngestSubject)
	}
	if cfg.OrdersDurable != "dur" || cfg.IngestMaxMsgs != 7 {
		t.Errorf("durable/maxmsgs = %q %d", cfg.OrdersDurable, cfg.IngestMaxMsgs)
	}
	if cfg.Crawl.Workers != 3 || cfg.Crawl.MaxDepth != 5 {
		t.Errorf("workers/depth = %d %d", cfg.Crawl.Workers, cfg.Crawl.MaxDepth)
	}
	if cfg.Crawl.CrawlDelay != 250*time.Millisecond {
		t.Errorf("delay = %v", cfg.Crawl.CrawlDelay)
	}
	if cfg.Crawl.UserAgent != "test-agent" {
		t.Errorf("user agent = %q", cfg.Crawl.UserAgent)
	}
}

func TestLoadServiceConfigRejectsInvalidValues(t *testing.T) {
	base := map[string]string{
		EnvNATSURL:  "nats://localhost:4222",
		EnvProxyURL: "http://proxy:4750",
	}
	cases := map[string]string{
		EnvWorkers:           "0",
		EnvMaxDepth:          "abc",
		EnvCrawlDelay:        "-1s",
		EnvNATSIngestMaxMsgs: "0",
	}
	for key, bad := range cases {
		env := map[string]string{}
		for k, v := range base {
			env[k] = v
		}
		env[key] = bad
		if _, err := LoadServiceConfig(envFrom(env)); err == nil {
			t.Errorf("%s=%q: expected error", key, bad)
		}
	}
}
