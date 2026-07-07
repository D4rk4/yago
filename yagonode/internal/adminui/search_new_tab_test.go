package adminui

import (
	"net/http"
	"strings"
	"testing"
)

func TestConsoleSearchLinksDefaultToSameTab(t *testing.T) {
	t.Parallel()

	console := New(Options{Search: fakeSearch{results: sampleResults()}})
	got := do(t, console, "/admin/search?q=go&scope=global")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	// The new-tab result rel is the precise same-tab-vs-new-tab discriminator;
	// checking for a bare target="_blank" would now also match the header brand
	// link, which is unrelated chrome.
	if strings.Contains(got.body, `rel="noopener noreferrer nofollow"`) {
		t.Fatal("same-tab default should not render new-tab result links")
	}
	if !strings.Contains(got.body, `rel="noreferrer nofollow"`) {
		t.Fatal("same-tab links should keep referrer and follow hygiene")
	}
}

func TestConsoleSearchLinksOpenNewTabWithIndicatorWhenEnabled(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Search:            fakeSearch{results: sampleResults()},
		SearchLinksNewTab: true,
	})
	got := do(t, console, "/admin/search?q=go&scope=global")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{
		`target="_blank"`,
		`rel="noopener noreferrer nofollow"`,
		"(opens in new tab)",
		"↗",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("new-tab search results missing %q", want)
		}
	}
}

func TestConsoleCrawlFormDefaultsEnableQueryURLsAndTLSAuthorityOptOut(t *testing.T) {
	t.Parallel()

	console := New(Options{Crawl: &fakeCrawl{}})
	got := do(t, console, "/admin/crawl")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{
		`name="allowQueryURLs" checked`,
		`name="ignoreTLSAuthority" checked`,
		"Ignore SSL certificate authority",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("crawl form missing %q", want)
		}
	}
}
