package yagocrawlcontract_test

import (
	"math"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestCrawlerRuntimePolicyProtoRoundTrip(t *testing.T) {
	policy := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	policy.AllowPrivateNetworks = true
	policy.AllowedPrivateCIDRs = []netip.Prefix{
		netip.MustParsePrefix("10.20.0.0/16"),
		netip.MustParsePrefix("fc00:12::/48"),
	}
	policy.CrawlDelay = 250 * time.Millisecond
	policy.BrowserPath = "/usr/bin/firefox-esr"
	policy.MetricsAddress = "127.0.0.1:9101"
	policy.BrowserSandbox = true
	message, err := yagocrawlcontract.CrawlerRuntimePolicyToProto(policy)
	if err != nil {
		t.Fatalf("encode policy: %v", err)
	}
	decoded, err := yagocrawlcontract.CrawlerRuntimePolicyFromProto(message)
	if err != nil {
		t.Fatalf("decode policy: %v", err)
	}
	if !policy.Equal(decoded) {
		t.Fatalf("decoded policy = %+v, want %+v", decoded, policy)
	}
}

func TestCrawlerRuntimePolicyLegacySandboxPresenceKeepsFallback(t *testing.T) {
	message, err := yagocrawlcontract.CrawlerRuntimePolicyToProto(
		yagocrawlcontract.DefaultCrawlerRuntimePolicy(),
	)
	if err != nil {
		t.Fatalf("encode policy: %v", err)
	}
	if message.BrowserSandbox == nil || message.GetBrowserSandbox() {
		t.Fatalf("current policy sandbox presence = %+v, want explicit false", message)
	}
	message.BrowserSandbox = nil
	fallback := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	fallback.BrowserSandbox = true
	decoded, err := yagocrawlcontract.CrawlerRuntimePolicyFromProtoWithFallback(
		message,
		fallback,
	)
	if err != nil {
		t.Fatalf("decode legacy policy: %v", err)
	}
	if !decoded.BrowserSandbox {
		t.Fatal("legacy policy erased the crawler sandbox bootstrap value")
	}
	decoded, err = yagocrawlcontract.CrawlerRuntimePolicyFromProto(message)
	if err != nil {
		t.Fatalf("decode legacy policy with canonical fallback: %v", err)
	}
	if decoded.BrowserSandbox {
		t.Fatal("canonical legacy fallback enabled the sandbox")
	}
}

func TestCrawlerRuntimePolicyLegacyFacilityPresenceKeepsFallback(t *testing.T) {
	message, err := yagocrawlcontract.CrawlerRuntimePolicyToProto(
		yagocrawlcontract.DefaultCrawlerRuntimePolicy(),
	)
	if err != nil {
		t.Fatalf("encode policy: %v", err)
	}
	if message.BrowserPath == nil || message.MetricsAddress == nil {
		t.Fatalf("current policy facility presence = %+v", message)
	}
	message.BrowserPath = nil
	message.MetricsAddress = nil
	fallback := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	fallback.BrowserPath = "/usr/bin/firefox-esr"
	fallback.MetricsAddress = "127.0.0.1:9101"
	decoded, err := yagocrawlcontract.CrawlerRuntimePolicyFromProtoWithFallback(
		message,
		fallback,
	)
	if err != nil {
		t.Fatalf("decode legacy policy: %v", err)
	}
	if decoded.BrowserPath != fallback.BrowserPath ||
		decoded.MetricsAddress != fallback.MetricsAddress {
		t.Fatalf("legacy facility fallback = %+v, want %+v", decoded, fallback)
	}
}

func TestCrawlerPrivateCIDRsStayInsidePrivateNetworks(t *testing.T) {
	prefixes, err := yagocrawlcontract.ParseCrawlerPrivateCIDRs(
		" 192.168.7.9/24,10.0.0.0/8,192.168.7.0/24 ",
	)
	if err != nil {
		t.Fatalf("parse private CIDRs: %v", err)
	}
	if got := yagocrawlcontract.FormatCrawlerPrivateCIDRs(prefixes); got !=
		"10.0.0.0/8,192.168.7.0/24" {
		t.Fatalf("canonical CIDRs = %q", got)
	}
	for _, raw := range []string{
		"not-a-cidr",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"100.64.0.0/10",
		"0.0.0.0/0",
		"fe80::/10",
		"8.8.8.0/24",
	} {
		if _, err := yagocrawlcontract.ParseCrawlerPrivateCIDRs(raw); err == nil {
			t.Errorf("ParseCrawlerPrivateCIDRs(%q) accepted a non-private range", raw)
		}
	}
}

func TestCrawlerRuntimePolicyRejectsOutOfBoundsValues(t *testing.T) {
	cases := []func(*yagocrawlcontract.CrawlerRuntimePolicy){
		func(policy *yagocrawlcontract.CrawlerRuntimePolicy) {
			policy.AllowedPrivateCIDRs = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}
		},
		func(policy *yagocrawlcontract.CrawlerRuntimePolicy) { policy.BrowserFailureThreshold = -1 },
		func(policy *yagocrawlcontract.CrawlerRuntimePolicy) { policy.BrowserPath = "firefox" },
		func(policy *yagocrawlcontract.CrawlerRuntimePolicy) {
			policy.BrowserPath = " /usr/bin/firefox-esr "
		},
		func(policy *yagocrawlcontract.CrawlerRuntimePolicy) { policy.ConnectTimeout = 0 },
		func(policy *yagocrawlcontract.CrawlerRuntimePolicy) { policy.CrawlDelay = -time.Millisecond },
		func(policy *yagocrawlcontract.CrawlerRuntimePolicy) { policy.HeaderTimeout = 3 * time.Hour },
		func(policy *yagocrawlcontract.CrawlerRuntimePolicy) { policy.MaximumDepth = 0 },
		func(policy *yagocrawlcontract.CrawlerRuntimePolicy) { policy.MaximumHostConcurrency = 0 },
		func(policy *yagocrawlcontract.CrawlerRuntimePolicy) { policy.MetricsAddress = "bad" },
		func(policy *yagocrawlcontract.CrawlerRuntimePolicy) {
			policy.MetricsAddress = "127.0.0.1:09101"
		},
		func(policy *yagocrawlcontract.CrawlerRuntimePolicy) { policy.RequestTimeout = 11 * time.Minute },
		func(policy *yagocrawlcontract.CrawlerRuntimePolicy) { policy.RunPagesPerMinute = 1_000_001 },
		func(policy *yagocrawlcontract.CrawlerRuntimePolicy) { policy.SitemapURLLimit = 0 },
		func(policy *yagocrawlcontract.CrawlerRuntimePolicy) { policy.TLSTimeout = time.Microsecond },
		func(policy *yagocrawlcontract.CrawlerRuntimePolicy) { policy.ShutdownGrace = 6 * time.Minute },
		func(policy *yagocrawlcontract.CrawlerRuntimePolicy) { policy.UserAgent = "bad\nagent" },
	}
	for index, mutate := range cases {
		policy := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
		mutate(&policy)
		if err := policy.Validate(); err == nil {
			t.Errorf("case %d accepted invalid policy", index)
		}
	}
}

func TestCrawlerRuntimePolicyProtoRejectsInvalidMessages(t *testing.T) {
	invalid := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	invalid.MaximumDepth = 0
	if _, err := yagocrawlcontract.CrawlerRuntimePolicyToProto(invalid); err == nil {
		t.Fatal("invalid runtime policy encoded")
	}
	invalidMessage, err := yagocrawlcontract.CrawlerRuntimePolicyToProto(
		yagocrawlcontract.DefaultCrawlerRuntimePolicy(),
	)
	if err != nil {
		t.Fatalf("encode valid runtime policy: %v", err)
	}
	validMaximumDepth := invalidMessage.MaximumDepth
	invalidMessage.MaximumDepth = 0
	if _, err := yagocrawlcontract.CrawlerRuntimePolicyFromProto(invalidMessage); err == nil {
		t.Fatal("invalid decoded runtime policy accepted")
	}
	invalidMessage.MaximumDepth = validMaximumDepth
	noncanonicalBrowserPath := " /usr/bin/firefox-esr "
	invalidMessage.BrowserPath = &noncanonicalBrowserPath
	if _, err := yagocrawlcontract.CrawlerRuntimePolicyFromProto(invalidMessage); err == nil {
		t.Fatal("noncanonical browser path decoded")
	}
	canonicalBrowserPath := ""
	noncanonicalMetricsAddress := "127.0.0.1:09101"
	invalidMessage.BrowserPath = &canonicalBrowserPath
	invalidMessage.MetricsAddress = &noncanonicalMetricsAddress
	if _, err := yagocrawlcontract.CrawlerRuntimePolicyFromProto(invalidMessage); err == nil {
		t.Fatal("noncanonical metrics address decoded")
	}
	if _, err := yagocrawlcontract.CrawlerRuntimePolicyFromProto(nil); err == nil {
		t.Fatal("missing runtime policy decoded")
	}
	if _, err := yagocrawlcontract.CrawlerRuntimePolicyFromProto(
		&crawlrpc.CrawlerRuntimePolicy{AllowedPrivateCidrs: []string{"not-a-cidr"}},
	); err == nil {
		t.Fatal("invalid private CIDR decoded")
	}
	if _, err := yagocrawlcontract.CrawlerRuntimePolicyFromProto(
		&crawlrpc.CrawlerRuntimePolicy{},
	); err == nil {
		t.Fatal("invalid runtime policy message decoded")
	}
	if _, err := yagocrawlcontract.CrawlerRuntimePolicyFromProto(
		&crawlrpc.CrawlerRuntimePolicy{
			ConnectTimeoutMilliseconds: math.MaxUint64,
		},
	); err == nil {
		t.Fatal("overflowing runtime policy duration decoded")
	}
}

func TestCrawlerRuntimePolicyBoundsPrivateCIDRList(t *testing.T) {
	values := make([]string, 0, yagocrawlcontract.MaximumCrawlerAllowedPrivateCIDRs+1)
	for index := range yagocrawlcontract.MaximumCrawlerAllowedPrivateCIDRs + 1 {
		values = append(values, netip.PrefixFrom(
			netip.AddrFrom4([4]byte{10, byte(index), 0, 0}),
			16,
		).String())
	}
	if _, err := yagocrawlcontract.ParseCrawlerPrivateCIDRs(
		strings.Join(values, ","),
	); err == nil {
		t.Fatal("oversized private CIDR list accepted")
	}
}
