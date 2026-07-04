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
	if cfg.CrawledPageIndexSubject != DefaultCrawledPageIndexSubject {
		t.Errorf("crawled page index subject = %q", cfg.CrawledPageIndexSubject)
	}
	if cfg.OrdersDurable != DefaultOrdersDurable {
		t.Errorf("durable = %q", cfg.OrdersDurable)
	}
	if cfg.CrawledPageIndexMaxMsgs != DefaultCrawledPageIndexMaxMsgs {
		t.Errorf("max msgs = %d", cfg.CrawledPageIndexMaxMsgs)
	}
	if orders := cfg.OrdersStreamSpec(); orders.Subject != cfg.OrdersSubject {
		t.Errorf("orders stream spec mismatch: %+v", orders)
	}
	if pageIndex := cfg.CrawledPageIndexStreamSpec(); pageIndex.Subject != cfg.CrawledPageIndexSubject ||
		pageIndex.MaxMsgs != cfg.CrawledPageIndexMaxMsgs {
		t.Errorf("crawled page index stream spec mismatch: %+v", pageIndex)
	}
	if cfg.CrawledPageEnabled {
		t.Error("crawled page sink should default to disabled")
	}
	if cfg.CrawledPageSubject != DefaultCrawledPageSubject {
		t.Errorf("crawled page subject = %q", cfg.CrawledPageSubject)
	}
	if cfg.CrawledPageMaxMsgs != DefaultCrawledPageMaxMsgs {
		t.Errorf("crawled page max msgs = %d", cfg.CrawledPageMaxMsgs)
	}
	if cfg.CrawledPageMaxBytes != DefaultCrawledPageMaxBytes {
		t.Errorf("crawled page max bytes = %d", cfg.CrawledPageMaxBytes)
	}
	pageSpec := cfg.CrawledPageStreamSpec()
	if pageSpec.Subject != cfg.CrawledPageSubject || pageSpec.MaxMsgs != cfg.CrawledPageMaxMsgs {
		t.Errorf("crawled page stream spec mismatch: %+v", pageSpec)
	}
}

func TestLoadServiceConfigCrawledPageOverrides(t *testing.T) {
	cfg, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNATSURL:                "nats://localhost:4222",
		EnvProxyURL:               "http://proxy:4750",
		EnvCrawledPageEnabled:     "true",
		EnvNATSCrawledPageSubject: "t.subject",
		EnvNATSCrawledPageMaxMsgs: "3",
		EnvCrawledPageMaxBytes:    "1024",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.CrawledPageEnabled {
		t.Error("expected crawled page sink enabled")
	}
	if cfg.CrawledPageSubject != "t.subject" || cfg.CrawledPageMaxMsgs != 3 ||
		cfg.CrawledPageMaxBytes != 1024 {
		t.Errorf("crawled page overrides = %+v", cfg)
	}
}

func TestLoadServiceConfigOverrides(t *testing.T) {
	cfg, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNATSURL:                     "nats://localhost:4222",
		EnvProxyURL:                    "http://proxy:4750",
		EnvNATSOrdersSubject:           "o.subject",
		EnvNATSCrawledPageIndexSubject: "i.subject",
		EnvNATSOrdersDurable:           "dur",
		EnvNATSCrawledPageIndexMaxMsgs: "7",
		EnvWorkers:                     "3",
		EnvMaxDepth:                    "5",
		EnvCrawlDelay:                  "250ms",
		EnvUserAgent:                   "test-agent",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.OrdersSubject != "o.subject" || cfg.CrawledPageIndexSubject != "i.subject" {
		t.Errorf("subjects = %q %q", cfg.OrdersSubject, cfg.CrawledPageIndexSubject)
	}
	if cfg.OrdersDurable != "dur" || cfg.CrawledPageIndexMaxMsgs != 7 {
		t.Errorf("durable/maxmsgs = %q %d", cfg.OrdersDurable, cfg.CrawledPageIndexMaxMsgs)
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
		EnvWorkers:                     "0",
		EnvMaxDepth:                    "abc",
		EnvCrawlDelay:                  "-1s",
		EnvNATSCrawledPageIndexMaxMsgs: "0",
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
