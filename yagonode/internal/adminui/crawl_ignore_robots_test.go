package adminui

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestConsoleCrawlStartThreadsIgnoreRobots: the explicit checkbox reaches the
// crawl source, and an unchecked form leaves robots enforced.
func TestConsoleCrawlStartThreadsIgnoreRobots(t *testing.T) {
	t.Parallel()

	crawl := &fakeCrawl{}
	console := New(Options{Crawl: crawl})
	got := doPost(t, console, "/admin/crawl", url.Values{
		"seeds":        {"https://example.org/"},
		"scope":        {"domain"},
		"maxDepth":     {"1"},
		"ignoreRobots": {"on"},
	})
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if !crawl.got.IgnoreRobots {
		t.Fatal("ignoreRobots checkbox did not reach the crawl source")
	}

	got = doPost(t, console, "/admin/crawl", url.Values{
		"seeds":    {"https://example.org/"},
		"scope":    {"domain"},
		"maxDepth": {"1"},
	})
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if crawl.got.IgnoreRobots {
		t.Fatal("robots must stay enforced without the explicit opt-out")
	}
}

// TestConsoleCrawlFormShowsRobotsOptOutUnchecked: the confirmation checkbox
// renders, and unlike the TLS toggle it is never pre-checked.
func TestConsoleCrawlFormShowsRobotsOptOutUnchecked(t *testing.T) {
	t.Parallel()

	console := New(Options{Crawl: &fakeCrawl{}})
	got := do(t, console, "/admin/crawl")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if !strings.Contains(got.body, `name="ignoreRobots"`) {
		t.Fatal("robots opt-out checkbox missing")
	}
	if strings.Contains(got.body, `name="ignoreRobots" checked`) {
		t.Fatal("robots opt-out must not be pre-checked")
	}
	if !strings.Contains(got.body, "Ignore robots.txt") {
		t.Fatal("robots opt-out label missing")
	}
}

// TestConsoleCrawlStartThreadsDisableBrowser: the fast-only opt-out reaches
// the crawl source and defaults to browser rendering staying available.
func TestConsoleCrawlStartThreadsDisableBrowser(t *testing.T) {
	t.Parallel()

	crawl := &fakeCrawl{}
	console := New(Options{Crawl: crawl})
	got := doPost(t, console, "/admin/crawl", url.Values{
		"seeds":          {"https://example.org/"},
		"scope":          {"domain"},
		"maxDepth":       {"1"},
		"disableBrowser": {"on"},
	})
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if !crawl.got.DisableBrowser {
		t.Fatal("disableBrowser checkbox did not reach the crawl source")
	}

	got = doPost(t, console, "/admin/crawl", url.Values{
		"seeds":    {"https://example.org/"},
		"scope":    {"domain"},
		"maxDepth": {"1"},
	})
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if crawl.got.DisableBrowser {
		t.Fatal("browser rendering must stay available by default")
	}
}
