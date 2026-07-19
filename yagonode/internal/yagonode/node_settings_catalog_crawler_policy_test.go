package yagonode

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestCrawlerRuntimePolicySettingsRoundTripAndApplyLive(t *testing.T) {
	policy := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	policy.UserAgent = "bootstrap-agent"
	base := nodeConfig{Crawl: crawlConfig{RuntimePolicy: policy}}
	values := map[string]string{
		settingKeyCrawlerAllowPrivateNetworks:      "true",
		settingKeyCrawlerAllowCIDRs:                "10.30.0.0/16",
		settingKeyCrawlerBrowserSandbox:            "true",
		settingKeyCrawlerBrowserFailureLimit:       "8",
		settingKeyCrawlerConnectTimeout:            "4s",
		settingKeyCrawlerCrawlDelay:                "250ms",
		settingKeyCrawlerFrontierStateMaximumBytes: "8GB",
		settingKeyCrawlerHeaderTimeout:             "3s",
		settingKeyCrawlerMaximumDepth:              "7",
		settingKeyCrawlerMaximumHostFetches:        "5",
		settingKeyCrawlerRequestTimeout:            "20s",
		settingKeyCrawlerRunPagesPerMinute:         "120",
		settingKeyCrawlerSitemapURLLimit:           "9000",
		settingKeyCrawlerTLSTimeout:                "2s",
		settingKeyCrawlerShutdownGrace:             "12s",
		settingKeyCrawlerUserAgent:                 "runtime-agent",
	}
	definitions := indexSettingDefinitions()
	effective := base
	for key, raw := range values {
		definition, found := definitions[key]
		if !found {
			t.Fatalf("crawler setting %q is missing", key)
		}
		if definition.restartRequired() {
			t.Fatalf("crawler setting %q does not propagate live", key)
		}
		normalized, err := definition.normalize(raw)
		if err != nil {
			t.Fatalf("normalize %s: %v", key, err)
		}
		effective = definition.apply(effective, normalized)
	}
	if err := effective.Crawl.RuntimePolicy.Validate(); err != nil {
		t.Fatalf("applied policy: %v", err)
	}

	toggles := newRuntimeToggles(base)
	live := policy
	toggles.SetCrawlerRuntimePolicySink(func(value yagocrawlcontract.CrawlerRuntimePolicy) bool {
		live = value
		return true
	})
	for key, raw := range values {
		normalized, _ := definitions[key].normalize(raw)
		definitions[key].applyLive(toggles, normalized)
	}
	if !live.Equal(effective.Crawl.RuntimePolicy) {
		t.Fatalf("live policy = %+v, want %+v", live, effective.Crawl.RuntimePolicy)
	}
	if live.CrawlDelay != 250*time.Millisecond || live.UserAgent != "runtime-agent" ||
		!live.BrowserSandbox {
		t.Fatalf("live policy values = %+v", live)
	}
}

func TestCrawlerRuntimePolicySettingValidation(t *testing.T) {
	definitions := indexSettingDefinitions()
	cases := map[string]string{
		settingKeyCrawlerAllowCIDRs:                "169.254.0.0/16",
		settingKeyCrawlerBrowserFailureLimit:       "1001",
		settingKeyCrawlerConnectTimeout:            "0s",
		settingKeyCrawlerCrawlDelay:                "1us",
		settingKeyCrawlerFrontierStateMaximumBytes: "invalid",
		settingKeyCrawlerHeaderTimeout:             "3h",
		settingKeyCrawlerMaximumDepth:              "65",
		settingKeyCrawlerMaximumHostFetches:        "0",
		settingKeyCrawlerRequestTimeout:            "11m",
		settingKeyCrawlerRunPagesPerMinute:         "1000001",
		settingKeyCrawlerSitemapURLLimit:           "0",
		settingKeyCrawlerTLSTimeout:                "soon",
		settingKeyCrawlerShutdownGrace:             "6m",
		settingKeyCrawlerUserAgent:                 "bad\nagent",
	}
	for key, raw := range cases {
		if _, err := definitions[key].normalize(raw); err == nil {
			t.Errorf("%s accepted %q", key, raw)
		}
	}
	if normalized, err := definitions[settingKeyCrawlerCrawlDelay].normalize(
		"0s",
	); err != nil ||
		normalized != "0s" {
		t.Fatalf("zero crawl delay = %q/%v", normalized, err)
	}
	if normalized, err := definitions[settingKeyCrawlerAllowCIDRs].normalize(
		" 192.168.5.2/24,10.0.0.0/8 ",
	); err != nil || normalized != "10.0.0.0/8,192.168.5.0/24" {
		t.Fatalf("private CIDRs = %q/%v", normalized, err)
	}
}

func TestCrawlerRuntimePolicyToggleRejectsInvalidMutation(t *testing.T) {
	policy := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	toggles := newRuntimeToggles(nodeConfig{Crawl: crawlConfig{RuntimePolicy: policy}})
	received := policy
	toggles.SetCrawlerRuntimePolicySink(func(value yagocrawlcontract.CrawlerRuntimePolicy) bool {
		received = value
		return true
	})
	toggles.UpdateCrawlerRuntimePolicy(func(value *yagocrawlcontract.CrawlerRuntimePolicy) {
		value.MaximumDepth = 0
	})
	if !received.Equal(policy) {
		t.Fatalf("invalid policy reached sink: %+v", received)
	}
	var nilToggles *runtimeToggles
	nilToggles.SetCrawlerRuntimePolicySink(nil)
	nilToggles.UpdateCrawlerRuntimePolicy(nil)
	updateCrawlerRuntimePolicy(nil, nil)
}
