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
	if cfg.MaxRedirects != DefaultMaxRedirects {
		t.Errorf("redirects = %d", cfg.MaxRedirects)
	}
	if cfg.SitemapURLLimit != DefaultSitemapURLLimit {
		t.Errorf("sitemap URL limit = %d", cfg.SitemapURLLimit)
	}
	if cfg.RequestTimeout != DefaultRequestTimeout ||
		cfg.ConnectTimeout != DefaultConnectTimeout ||
		cfg.TLSTimeout != DefaultTLSTimeout ||
		cfg.HeaderTimeout != DefaultHeaderTimeout {
		t.Errorf("default timeouts = %+v", cfg)
	}
}

func TestLoadServiceConfigRequiresNATSURL(t *testing.T) {
	if _, err := LoadServiceConfig(envFrom(nil)); err == nil {
		t.Fatal("expected error when NATS_URL is unset")
	}
}

func TestLoadServiceConfigAllowsMissingEgressVars(t *testing.T) {
	cfg, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNATSURL: "nats://localhost:4222",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.EgressAllowLAN {
		t.Error("EgressAllowLAN must default to false")
	}
}

func TestLoadServiceConfigRejectsBadEgressAllowLAN(t *testing.T) {
	if _, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNATSURL:        "nats://localhost:4222",
		EnvEgressAllowLAN: "maybe",
	})); err == nil {
		t.Fatal("expected error for malformed egress toggle")
	}
}

func TestLoadServiceConfigDefaults(t *testing.T) {
	cfg, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNATSURL: "nats://localhost:4222",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.EgressAllowLAN {
		t.Error("EgressAllowLAN must default to false")
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
	if cfg.Crawl.MaxRedirects != DefaultMaxRedirects {
		t.Errorf("redirects = %d", cfg.Crawl.MaxRedirects)
	}
	if cfg.Crawl.SitemapURLLimit != DefaultSitemapURLLimit {
		t.Errorf("sitemap URL limit = %d", cfg.Crawl.SitemapURLLimit)
	}
	if cfg.Crawl.RequestTimeout != DefaultRequestTimeout ||
		cfg.Crawl.ConnectTimeout != DefaultConnectTimeout ||
		cfg.Crawl.TLSTimeout != DefaultTLSTimeout ||
		cfg.Crawl.HeaderTimeout != DefaultHeaderTimeout {
		t.Errorf("timeouts = %+v", cfg.Crawl)
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
		EnvEgressAllowLAN:    "true",
		EnvNATSOrdersSubject: "o.subject",
		EnvNATSIngestSubject: "i.subject",
		EnvNATSDurable:       "dur",
		EnvNATSIngestMaxMsgs: "7",
		EnvWorkers:           "3",
		EnvMaxDepth:          "5",
		EnvCrawlDelay:        "250ms",
		EnvUserAgent:         "test-agent",
		EnvRequestTimeout:    "20s",
		EnvConnectTimeout:    "4s",
		EnvTLSTimeout:        "3s",
		EnvHeaderTimeout:     "2s",
		EnvMaxRedirects:      "2",
		EnvSitemapURLLimit:   "9",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.EgressAllowLAN {
		t.Error("EgressAllowLAN = false, want true")
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
	if cfg.Crawl.MaxRedirects != 2 {
		t.Errorf("redirects = %d", cfg.Crawl.MaxRedirects)
	}
	if cfg.Crawl.SitemapURLLimit != 9 {
		t.Errorf("sitemap URL limit = %d", cfg.Crawl.SitemapURLLimit)
	}
	if cfg.Crawl.RequestTimeout != 20*time.Second ||
		cfg.Crawl.ConnectTimeout != 4*time.Second ||
		cfg.Crawl.TLSTimeout != 3*time.Second ||
		cfg.Crawl.HeaderTimeout != 2*time.Second {
		t.Errorf("timeouts = %+v", cfg.Crawl)
	}
}

func TestLoadServiceConfigRejectsInvalidValues(t *testing.T) {
	base := map[string]string{
		EnvNATSURL: "nats://localhost:4222",
	}
	cases := map[string]string{
		EnvWorkers:           "0",
		EnvMaxDepth:          "abc",
		EnvCrawlDelay:        "-1s",
		EnvNATSIngestMaxMsgs: "0",
		EnvMaxRedirects:      "-1",
		EnvRequestTimeout:    "0s",
		EnvConnectTimeout:    "0s",
		EnvTLSTimeout:        "0s",
		EnvHeaderTimeout:     "0s",
		EnvSitemapURLLimit:   "0",
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

func TestLoadServiceConfigRejectsParseErrors(t *testing.T) {
	base := map[string]string{
		EnvNATSURL: "nats://localhost:4222",
	}
	cases := map[string]string{
		EnvNATSIngestMaxMsgs: "abc",
		EnvCrawlDelay:        "not-a-duration",
		EnvRequestTimeout:    "not-a-duration",
		EnvConnectTimeout:    "not-a-duration",
		EnvTLSTimeout:        "not-a-duration",
		EnvHeaderTimeout:     "not-a-duration",
		EnvMaxRedirects:      "not-a-number",
		EnvSitemapURLLimit:   "not-a-number",
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
