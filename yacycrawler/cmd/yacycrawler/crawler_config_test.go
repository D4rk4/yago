package main

import (
	"net/netip"
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

func TestLoadServiceConfigRequiresNodeRPCAddr(t *testing.T) {
	if _, err := LoadServiceConfig(envFrom(nil)); err == nil {
		t.Fatal("expected error when node RPC address is unset")
	}
}

func TestLoadServiceConfigRejectsBadEgressAllowLAN(t *testing.T) {
	if _, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNodeRPCAddr:    "node:9091",
		EnvEgressAllowLAN: "maybe",
	})); err == nil {
		t.Fatal("expected error for malformed egress toggle")
	}
}

func TestLoadServiceConfigRejectsBadEgressCIDR(t *testing.T) {
	if _, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNodeRPCAddr:      "node:9091",
		EnvEgressAllowCIDRs: "10.0.0.0/8,not-a-cidr",
	})); err == nil {
		t.Fatal("expected error for malformed egress CIDR")
	}
}

func TestLoadServiceConfigReadsEgressCIDRs(t *testing.T) {
	cfg, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNodeRPCAddr:      "node:9091",
		EnvEgressAllowCIDRs: " 10.10.5.0/16 , 192.168.0.0/24 ",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := []netip.Prefix{
		netip.MustParsePrefix("10.10.0.0/16"),
		netip.MustParsePrefix("192.168.0.0/24"),
	}
	if len(cfg.EgressAllowedCIDRs) != len(want) {
		t.Fatalf("EgressAllowedCIDRs = %v, want %v", cfg.EgressAllowedCIDRs, want)
	}
	for i, prefix := range want {
		if cfg.EgressAllowedCIDRs[i] != prefix {
			t.Errorf("cidr[%d] = %v, want %v", i, cfg.EgressAllowedCIDRs[i], prefix)
		}
	}
}

func TestLoadServiceConfigDefaultsEgressCIDRsEmpty(t *testing.T) {
	cfg, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNodeRPCAddr: "node:9091",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.EgressAllowedCIDRs != nil {
		t.Errorf("EgressAllowedCIDRs = %v, want nil by default", cfg.EgressAllowedCIDRs)
	}
}

func TestLoadServiceConfigDefaults(t *testing.T) {
	cfg, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNodeRPCAddr: "node:9091",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.NodeRPCAddr != "node:9091" {
		t.Errorf("node RPC addr = %q", cfg.NodeRPCAddr)
	}
	if cfg.WorkerID != DefaultWorkerID {
		t.Errorf("worker id = %q, want %q", cfg.WorkerID, DefaultWorkerID)
	}
	if cfg.EgressAllowLAN {
		t.Error("EgressAllowLAN must default to false")
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
}

func TestLoadServiceConfigOverrides(t *testing.T) {
	cfg, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNodeRPCAddr:     "node:9091",
		EnvWorkerID:        "worker-7",
		EnvEgressAllowLAN:  "true",
		EnvWorkers:         "3",
		EnvMaxDepth:        "5",
		EnvCrawlDelay:      "250ms",
		EnvUserAgent:       "test-agent",
		EnvRequestTimeout:  "20s",
		EnvConnectTimeout:  "4s",
		EnvTLSTimeout:      "3s",
		EnvHeaderTimeout:   "2s",
		EnvMaxRedirects:    "2",
		EnvSitemapURLLimit: "9",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.WorkerID != "worker-7" {
		t.Errorf("worker id = %q, want worker-7", cfg.WorkerID)
	}
	if !cfg.EgressAllowLAN {
		t.Error("EgressAllowLAN = false, want true")
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
	base := map[string]string{EnvNodeRPCAddr: "node:9091"}
	cases := map[string]string{
		EnvWorkers:         "0",
		EnvMaxDepth:        "abc",
		EnvCrawlDelay:      "-1s",
		EnvMaxRedirects:    "-1",
		EnvRequestTimeout:  "0s",
		EnvConnectTimeout:  "0s",
		EnvTLSTimeout:      "0s",
		EnvHeaderTimeout:   "0s",
		EnvSitemapURLLimit: "0",
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
	base := map[string]string{EnvNodeRPCAddr: "node:9091"}
	cases := map[string]string{
		EnvCrawlDelay:      "not-a-duration",
		EnvRequestTimeout:  "not-a-duration",
		EnvConnectTimeout:  "not-a-duration",
		EnvTLSTimeout:      "not-a-duration",
		EnvHeaderTimeout:   "not-a-duration",
		EnvMaxRedirects:    "not-a-number",
		EnvSitemapURLLimit: "not-a-number",
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
