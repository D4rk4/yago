//go:build firefox_integration

// Package firefoxfetch's end-to-end check drives a real headless Firefox over
// Marionette through the production egress guard. It is tag-gated out of the
// default `make verify` because it needs a Firefox binary and outbound network;
// run it with:
//
//	go test -tags firefox_integration ./yagocrawler/internal/firefoxfetch/
//
// It targets the IANA reserved example domains (stable, low-traffic) to prove
// the whole slow path: launch, Marionette session, proxied navigation past the
// guard, DOM extraction, and content-type gating — with two fetches served by
// one long-lived process.
package firefoxfetch

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagoegress"
)

func TestFirefoxFetchesRealPagesThroughOneProcess(t *testing.T) {
	if _, err := firefoxBinary(""); err != nil {
		t.Skipf("no firefox binary: %v", err)
	}

	fetcher, closeFetcher, err := NewBrowserPageFetcher(
		BrowserLaunch{
			UserAgent: "yago-crawler-integration/1.0",
			Timeout:   60 * time.Second,
			MaxBytes:  1 << 20,
		},
		// The production guard: blocks private/loopback, allows public hosts.
		yagoegress.NewGuard(false),
	)
	if err != nil {
		t.Fatalf("new fetcher: %v", err)
	}
	defer closeFetcher()

	ctx := context.Background()
	// Two public fetches, one long-lived Firefox: proves the persistent-process
	// model and that the egress-guarded proxy path renders real pages.
	for _, target := range []string{"https://example.com/", "https://example.net/"} {
		page, err := fetcher.Fetch(ctx, mustParse(t, target))
		if err != nil {
			t.Fatalf("fetch %s: %v", target, err)
		}
		if !strings.HasPrefix(page.ContentType, "text/html") {
			t.Fatalf("%s content type = %q, want text/html", target, page.ContentType)
		}
		if !strings.Contains(strings.ToLower(string(page.Body)), "example domain") {
			t.Fatalf("%s body missing expected marker (%d bytes)", target, len(page.Body))
		}
	}
}
