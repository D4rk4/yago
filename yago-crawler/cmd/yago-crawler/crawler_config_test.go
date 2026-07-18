package main

import (
	"net/netip"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func envFrom(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestDefaultCrawlConfig(t *testing.T) {
	cfg := DefaultCrawlConfig()
	if cfg.Workers <= 0 || cfg.JobQueueSize <= 0 || cfg.MaxBodyBytes <= 0 {
		t.Errorf("default config has non-positive bounds: %+v", cfg)
	}
	if !cfg.PrioritizeAutomaticDiscovery {
		t.Error("automatic discovery priority must default on")
	}
	if cfg.MaxRedirects != DefaultMaxRedirects {
		t.Errorf("redirects = %d", cfg.MaxRedirects)
	}
	if cfg.SitemapURLLimit != DefaultSitemapURLLimit {
		t.Errorf("sitemap URL limit = %d", cfg.SitemapURLLimit)
	}
	if cfg.BrowserFailureThreshold != pagefetch.DefaultBrowserFailureThreshold {
		t.Errorf("browser failure threshold = %d, want %d",
			cfg.BrowserFailureThreshold, pagefetch.DefaultBrowserFailureThreshold)
	}
	if cfg.MaxPagesPerRun != DefaultMaxPagesPerRun {
		t.Errorf("max pages per run = %d, want %d", cfg.MaxPagesPerRun, DefaultMaxPagesPerRun)
	}
	if cfg.RequestTimeout != DefaultRequestTimeout ||
		cfg.ConnectTimeout != DefaultConnectTimeout ||
		cfg.TLSTimeout != DefaultTLSTimeout ||
		cfg.HeaderTimeout != DefaultHeaderTimeout {
		t.Errorf("default timeouts = %+v", cfg)
	}
}

func TestLoadServiceConfigBoundsFetchWorkers(t *testing.T) {
	if _, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNodeRPCAddr: "node:9091",
		EnvWorkers:     "257",
	})); err == nil {
		t.Fatal("expected excessive fetch-worker concurrency to fail")
	}
	config, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNodeRPCAddr: "node:9091",
		EnvWorkers:     "256",
	}))
	if err != nil {
		t.Fatalf("load maximum fetch-worker concurrency: %v", err)
	}
	if config.Crawl.Workers != yagocrawlcontract.MaximumFetchWorkerConcurrency {
		t.Fatalf("workers = %d, want %d", config.Crawl.Workers,
			yagocrawlcontract.MaximumFetchWorkerConcurrency)
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

func TestLoadServiceConfigRejectsBadShutdownGrace(t *testing.T) {
	if _, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNodeRPCAddr:   "node:9091",
		EnvShutdownGrace: "soon",
	})); err == nil {
		t.Fatal("expected error for a bad shutdown grace")
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
	if cfg.DataDir != DefaultDataDir {
		t.Errorf("data dir = %q, want %q", cfg.DataDir, DefaultDataDir)
	}
	if cfg.MetricsAddr != "" {
		t.Errorf("metrics addr = %q, want empty", cfg.MetricsAddr)
	}
	if cfg.ShutdownGrace != DefaultShutdownGrace {
		t.Errorf("shutdown grace = %v, want %v", cfg.ShutdownGrace, DefaultShutdownGrace)
	}
	if cfg.EgressAllowLAN {
		t.Error("EgressAllowLAN must default to false")
	}
	if cfg.Crawl.MaxRedirects != DefaultMaxRedirects {
		t.Errorf("redirects = %d", cfg.Crawl.MaxRedirects)
	}
	if cfg.Crawl.MaxHostConcurrency != DefaultMaxHostConcurrency {
		t.Errorf("max host concurrency = %d, want %d",
			cfg.Crawl.MaxHostConcurrency, DefaultMaxHostConcurrency)
	}
	if cfg.Crawl.SitemapURLLimit != DefaultSitemapURLLimit {
		t.Errorf("sitemap URL limit = %d", cfg.Crawl.SitemapURLLimit)
	}
	if cfg.Crawl.MaxPagesPerRun != DefaultMaxPagesPerRun {
		t.Errorf("max pages per run = %d, want %d", cfg.Crawl.MaxPagesPerRun, DefaultMaxPagesPerRun)
	}
	if cfg.Crawl.RunPagesPerMinute != DefaultRunPagesPerMinute {
		t.Errorf("run pages per minute = %d, want %d",
			cfg.Crawl.RunPagesPerMinute, DefaultRunPagesPerMinute)
	}
	if cfg.Crawl.RequestTimeout != DefaultRequestTimeout ||
		cfg.Crawl.ConnectTimeout != DefaultConnectTimeout ||
		cfg.Crawl.TLSTimeout != DefaultTLSTimeout ||
		cfg.Crawl.HeaderTimeout != DefaultHeaderTimeout {
		t.Errorf("timeouts = %+v", cfg.Crawl)
	}
	if cfg.Crawl.BrowserPath != "" {
		t.Errorf("browser path = %q, want empty (PATH discovery)", cfg.Crawl.BrowserPath)
	}
	if cfg.Crawl.BrowserSandbox {
		t.Error("browser sandbox must default off so containers and userns-restricted hosts start")
	}
	if cfg.Crawl.BrowserFailureThreshold != pagefetch.DefaultBrowserFailureThreshold {
		t.Errorf("browser failure threshold = %d, want %d",
			cfg.Crawl.BrowserFailureThreshold, pagefetch.DefaultBrowserFailureThreshold)
	}
}

func TestLoadServiceConfigOverrides(t *testing.T) {
	cfg, err := LoadServiceConfig(envFrom(overrideServiceEnvironment()))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Crawl.BrowserPath != "/usr/bin/firefox-esr" {
		t.Errorf("browser path = %q, want /usr/bin/firefox-esr", cfg.Crawl.BrowserPath)
	}
	if !cfg.Crawl.BrowserSandbox {
		t.Error("browser sandbox = false, want true when the host opts in")
	}
	if cfg.Crawl.BrowserFailureThreshold != 9 {
		t.Errorf("browser failure threshold = %d, want 9", cfg.Crawl.BrowserFailureThreshold)
	}
	if cfg.WorkerID != "worker-7" {
		t.Errorf("worker id = %q, want worker-7", cfg.WorkerID)
	}
	if cfg.DataDir != "/srv/yago" {
		t.Errorf("data dir = %q, want /srv/yago", cfg.DataDir)
	}
	if cfg.MetricsAddr != "127.0.0.1:9100" {
		t.Errorf("metrics addr = %q, want 127.0.0.1:9100", cfg.MetricsAddr)
	}
	if cfg.ShutdownGrace != 5*time.Second {
		t.Errorf("shutdown grace = %v, want 5s", cfg.ShutdownGrace)
	}
	if !cfg.EgressAllowLAN {
		t.Error("EgressAllowLAN = false, want true")
	}
	if cfg.Crawl.Workers != 3 || cfg.Crawl.MaxDepth != 5 {
		t.Errorf("workers/depth = %d %d", cfg.Crawl.Workers, cfg.Crawl.MaxDepth)
	}
	if cfg.Crawl.PrioritizeAutomaticDiscovery {
		t.Error("automatic discovery priority = true, want false override")
	}
	if cfg.Crawl.MaxPagesPerRun != 12345 {
		t.Errorf("max pages per run = %d, want 12345", cfg.Crawl.MaxPagesPerRun)
	}
	if cfg.Crawl.MaxHostConcurrency != 6 {
		t.Errorf("max host concurrency = %d, want 6", cfg.Crawl.MaxHostConcurrency)
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

func overrideServiceEnvironment() map[string]string {
	return map[string]string{
		EnvNodeRPCAddr:                  "node:9091",
		EnvDataDir:                      "/srv/yago",
		EnvWorkerID:                     "worker-7",
		EnvMetricsAddr:                  "127.0.0.1:9100",
		EnvShutdownGrace:                "5s",
		EnvEgressAllowLAN:               "true",
		EnvWorkers:                      "3",
		EnvPrioritizeAutomaticDiscovery: "false",
		EnvMaxHostConcurrency:           "6",
		EnvMaxDepth:                     "5",
		EnvMaxPagesPerRun:               "12345",
		EnvCrawlDelay:                   "250ms",
		EnvUserAgent:                    "test-agent",
		EnvRequestTimeout:               "20s",
		EnvConnectTimeout:               "4s",
		EnvTLSTimeout:                   "3s",
		EnvHeaderTimeout:                "2s",
		EnvMaxRedirects:                 "2",
		EnvSitemapURLLimit:              "9",
		EnvBrowserPath:                  "/usr/bin/firefox-esr",
		EnvBrowserSandbox:               "true",
		EnvBrowserFailureThreshold:      "9",
	}
}

func TestLoadServiceConfigRejectsInvalidValues(t *testing.T) {
	base := map[string]string{EnvNodeRPCAddr: "node:9091"}
	cases := map[string]string{
		EnvWorkers:         "0",
		EnvMaxDepth:        "abc",
		EnvMaxPagesPerRun:  "-5",
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
		EnvCrawlDelay:                   "not-a-duration",
		EnvRequestTimeout:               "not-a-duration",
		EnvConnectTimeout:               "not-a-duration",
		EnvTLSTimeout:                   "not-a-duration",
		EnvHeaderTimeout:                "not-a-duration",
		EnvMaxRedirects:                 "not-a-number",
		EnvSitemapURLLimit:              "not-a-number",
		EnvMaxHostConcurrency:           "not-a-number",
		EnvBrowserSandbox:               "not-a-bool",
		EnvBrowserFailureThreshold:      "not-a-number",
		EnvPrioritizeAutomaticDiscovery: "not-a-bool",
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

// TestLoadServiceConfigRunRateOverride: the per-run default rate is tunable and
// an explicit zero disables the default pacing entirely.
func TestLoadServiceConfigRunRateOverride(t *testing.T) {
	cfg, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNodeRPCAddr:       "node:9091",
		EnvRunPagesPerMinute: "120",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Crawl.RunPagesPerMinute != 120 {
		t.Errorf("run pages per minute = %d, want 120", cfg.Crawl.RunPagesPerMinute)
	}

	unlimited, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNodeRPCAddr:       "node:9091",
		EnvRunPagesPerMinute: "0",
	}))
	if err != nil {
		t.Fatalf("load zero: %v", err)
	}
	if unlimited.Crawl.RunPagesPerMinute != 0 {
		t.Errorf("run pages per minute = %d, want 0", unlimited.Crawl.RunPagesPerMinute)
	}

	if _, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNodeRPCAddr:       "node:9091",
		EnvRunPagesPerMinute: "-5",
	})); err == nil {
		t.Fatal("negative run rate must be rejected")
	}
}
