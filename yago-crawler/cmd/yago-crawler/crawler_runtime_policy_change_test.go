package main

import (
	"net/netip"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type browserSandboxPolicyRecorder struct {
	values []bool
}

func (recorder *browserSandboxPolicyRecorder) SetSandbox(value bool) {
	recorder.values = append(recorder.values, value)
}

func TestCrawlerRuntimePolicyChangeAppliesSandboxWithoutRestart(t *testing.T) {
	effective := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	effective.AllowedPrivateCIDRs = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	browser := &browserSandboxPolicyRecorder{}
	restarts := 0
	change := newCrawlerRuntimePolicyChange(effective, browser, func() { restarts++ })

	change.Apply(effective)
	updated := effective
	updated.BrowserSandbox = true
	change.Apply(updated)
	change.Apply(updated)

	if restarts != 0 || len(browser.values) != 1 || !browser.values[0] {
		t.Fatalf("sandbox application = %v, restarts = %d", browser.values, restarts)
	}
	current := change.Current()
	if !current.Equal(updated) {
		t.Fatalf("effective policy = %+v, want %+v", current, updated)
	}
	current.AllowedPrivateCIDRs[0] = netip.MustParsePrefix("10.2.0.0/16")
	if !change.Current().AllowedPrivateCIDRs[0].Contains(
		netip.MustParseAddr("10.1.0.1"),
	) {
		t.Fatal("effective policy escaped through the current snapshot")
	}
}

func TestCrawlerRuntimePolicyChangeRestartsForNonSandboxChange(t *testing.T) {
	effective := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	browser := &browserSandboxPolicyRecorder{}
	restarts := 0
	change := newCrawlerRuntimePolicyChange(effective, browser, func() { restarts++ })
	updated := effective
	updated.BrowserSandbox = true
	updated.CrawlDelay = 2 * time.Second

	change.Apply(updated)

	if restarts != 1 || len(browser.values) != 0 || !change.Current().Equal(effective) {
		t.Fatalf(
			"mixed policy application = %v, restarts = %d, current = %+v",
			browser.values,
			restarts,
			change.Current(),
		)
	}
}

func TestCrawlerRuntimePolicyChangeRestartsWhenBrowserCannotApplySandbox(t *testing.T) {
	effective := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	restarts := 0
	change := newCrawlerRuntimePolicyChange(effective, nil, func() { restarts++ })
	updated := effective
	updated.BrowserSandbox = true

	change.Apply(updated)

	if restarts != 1 || !change.Current().Equal(effective) {
		t.Fatalf("sandbox fallback restarts = %d, current = %+v", restarts, change.Current())
	}
}
