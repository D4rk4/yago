package adminui

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestConsoleCrawlStartThreadsNoindexCanonicalMismatch: the CRAWL-29 checkbox
// reaches the crawl source, and an unchecked form keeps canonical-mismatching
// pages indexed.
func TestConsoleCrawlStartThreadsNoindexCanonicalMismatch(t *testing.T) {
	t.Parallel()

	crawl := &fakeCrawl{}
	console := New(Options{Crawl: crawl})
	got := doPost(t, console, "/admin/crawl", url.Values{
		"seeds":                    {"https://example.org/"},
		"scope":                    {"domain"},
		"maxDepth":                 {"1"},
		"noindexCanonicalMismatch": {"on"},
	})
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if !crawl.got.NoindexCanonicalMismatch {
		t.Fatal("noindexCanonicalMismatch checkbox did not reach the crawl source")
	}

	got = doPost(t, console, "/admin/crawl", url.Values{
		"seeds":    {"https://example.org/"},
		"scope":    {"domain"},
		"maxDepth": {"1"},
	})
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if crawl.got.NoindexCanonicalMismatch {
		t.Fatal("canonical-mismatch pages must stay indexed without the opt-in")
	}
}

// TestConsoleCrawlFormShowsCanonicalMismatchUnchecked: the opt-in renders and
// is never pre-checked, since canonical often points paginated pages at page 1.
func TestConsoleCrawlFormShowsCanonicalMismatchUnchecked(t *testing.T) {
	t.Parallel()

	console := New(Options{Crawl: &fakeCrawl{}})
	got := do(t, console, "/admin/crawl")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if !strings.Contains(got.body, `name="noindexCanonicalMismatch"`) {
		t.Fatal("canonical-mismatch checkbox missing")
	}
	if strings.Contains(got.body, `name="noindexCanonicalMismatch" checked`) {
		t.Fatal("canonical-mismatch opt-in must not be pre-checked")
	}
	if !strings.Contains(got.body, "rel=canonical") {
		t.Fatal("canonical-mismatch label missing")
	}
}
