package yagonode

import (
	"math"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestLoadCrawlerRuntimePolicyReadsEveryBootstrapControl(t *testing.T) {
	environment := map[string]string{
		envCrawlerAllowPrivateNetworks:      "true",
		envCrawlerAllowCIDRs:                "10.20.0.0/16,fc00:20::/48",
		envCrawlerBrowserSandbox:            "true",
		envCrawlerBrowserFailureLimit:       "8",
		envCrawlerBrowserPath:               "/usr/bin/firefox-esr",
		envCrawlerConnectTimeout:            "4s",
		envCrawlerCrawlDelay:                "250ms",
		envCrawlerFrontierStateMaximumBytes: "8GB",
		envCrawlerHeaderTimeout:             "3s",
		envCrawlerMaximumDepth:              "7",
		envCrawlerMaximumHostFetches:        "5",
		envCrawlerMetricsAddress:            "127.0.0.1:9101",
		envCrawlerRequestTimeout:            "20s",
		envCrawlerRunPagesPerMinute:         "120",
		envCrawlerSitemapURLLimit:           "9000",
		envCrawlerTLSTimeout:                "2s",
		envCrawlerShutdownGrace:             "12s",
		envCrawlerUserAgent:                 "crawler-policy-test",
	}
	config, err := loadCrawlConfig(func(key string) string { return environment[key] })
	if err != nil {
		t.Fatalf("load crawl config: %v", err)
	}
	policy := config.RuntimePolicy
	if !policy.AllowPrivateNetworks ||
		yagocrawlcontract.FormatCrawlerPrivateCIDRs(policy.AllowedPrivateCIDRs) !=
			"10.20.0.0/16,fc00:20::/48" ||
		!policy.BrowserSandbox || policy.BrowserFailureThreshold != 8 ||
		policy.BrowserPath != "/usr/bin/firefox-esr" ||
		policy.ConnectTimeout != 4*time.Second ||
		policy.CrawlDelay != 250*time.Millisecond || policy.HeaderTimeout != 3*time.Second ||
		policy.FrontierStateMaximumBytes != 8<<30 ||
		policy.MaximumDepth != 7 || policy.MaximumHostConcurrency != 5 ||
		policy.MetricsAddress != "127.0.0.1:9101" ||
		policy.RequestTimeout != 20*time.Second || policy.RunPagesPerMinute != 120 ||
		policy.SitemapURLLimit != 9000 || policy.TLSTimeout != 2*time.Second ||
		policy.ShutdownGrace != 12*time.Second || policy.UserAgent != "crawler-policy-test" {
		t.Fatalf("crawler runtime policy = %+v", policy)
	}
}

func TestLoadCrawlerRuntimePolicyUsesCanonicalDefaults(t *testing.T) {
	policy, err := loadCrawlerRuntimePolicy(func(string) string { return "" })
	if err != nil {
		t.Fatalf("load default crawler runtime policy: %v", err)
	}
	want := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	want.UserAgent = "yago-crawler/" + Version() + " (+https://github.com/D4rk4/yago/)"
	if !policy.Equal(want) || policy.CrawlDelay != time.Second {
		t.Fatalf("default runtime policy = %+v, want %+v", policy, want)
	}
}

func TestLoadCrawlerRuntimePolicyAcceptsDisabledFrontierStateMaximum(t *testing.T) {
	policy, err := loadCrawlerRuntimePolicy(func(name string) string {
		if name == envCrawlerFrontierStateMaximumBytes {
			return "0"
		}

		return ""
	})
	if err != nil || policy.FrontierStateMaximumBytes != 0 {
		t.Fatalf("disabled frontier state maximum = %d, %v", policy.FrontierStateMaximumBytes, err)
	}
}

func TestCrawlerFrontierStateMaximumByteSizeBounds(t *testing.T) {
	raw := formatCrawlerFrontierStateMaximumBytes(math.MaxUint64)
	if raw != "18446744073709551615B" {
		t.Fatalf("maximum frontier state byte size = %q", raw)
	}
	if _, err := parseCrawlerFrontierStateMaximumBytes(raw); err == nil {
		t.Fatal("frontier state byte size above the supported filesystem range accepted")
	}
}

func TestLoadCrawlerRuntimePolicyRejectsEveryInvalidBootstrapControl(t *testing.T) {
	cases := map[string]string{
		envCrawlerAllowPrivateNetworks:      "sometimes",
		envCrawlerAllowCIDRs:                "127.0.0.0/8",
		envCrawlerBrowserSandbox:            "sometimes",
		envCrawlerBrowserFailureLimit:       "1001",
		envCrawlerBrowserPath:               "firefox-esr",
		envCrawlerConnectTimeout:            "0s",
		envCrawlerCrawlDelay:                "-1s",
		envCrawlerFrontierStateMaximumBytes: "invalid",
		envCrawlerHeaderTimeout:             "soon",
		envCrawlerMaximumDepth:              "65",
		envCrawlerMaximumHostFetches:        "257",
		envCrawlerMetricsAddress:            "localhost:9101",
		envCrawlerRequestTimeout:            "soon",
		envCrawlerRunPagesPerMinute:         "1000001",
		envCrawlerSitemapURLLimit:           "1000001",
		envCrawlerTLSTimeout:                "soon",
		envCrawlerShutdownGrace:             "soon",
		envCrawlerUserAgent:                 "bad\nagent",
	}
	for key, value := range cases {
		t.Run(key, func(t *testing.T) {
			if _, err := loadCrawlerRuntimePolicy(func(name string) string {
				if name == key {
					return value
				}
				return ""
			}); err == nil {
				t.Fatalf("%s=%q accepted", key, value)
			}
		})
	}
}

func TestCrawlerDurationEnvRejectsSyntax(t *testing.T) {
	if _, err := crawlerDurationEnv(
		func(string) string { return "soon" },
		"duration",
		time.Second,
		false,
	); err == nil {
		t.Fatal("invalid duration accepted")
	}
}
