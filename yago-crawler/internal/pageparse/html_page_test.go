package pageparse

import (
	"strings"
	"testing"
)

func TestParseHTMLPageExtractsVisibleTextAndBoundedLinks(t *testing.T) {
	page := ParseHTMLPage("https://example.org/start", []byte(`<html lang="en"><head>
<title>Start page</title><link rel="canonical" href="/canonical">
<link rel="alternate nofollow" type="application/rss+xml" href="/feed.xml">
<meta name="robots" content="noindex, nofollow">
</head><body><p>Visible content.</p><script>hidden script phrase</script>
<a href="/next">Next page</a></body></html>`))
	if page.Title != "Start page" || page.Language != "en" ||
		page.CanonicalURL != "https://example.org/canonical" || !page.Noindex || !page.Nofollow {
		t.Fatalf("page fields = %#v", page)
	}
	if strings.Contains(page.Text, "hidden script phrase") || !strings.Contains(page.Text, "Visible content.") {
		t.Fatalf("visible text = %q", page.Text)
	}
	if len(page.Links) != 2 || page.Links[0].URL != "https://example.org/next" ||
		page.Links[1].URL != "https://example.org/feed.xml" || !page.Links[0].NoFollow ||
		!page.Links[1].NoFollow {
		t.Fatalf("links = %#v", page.Links)
	}
}

func TestParseHTMLPageRejectsUnsafeAndDuplicateLinks(t *testing.T) {
	page := ParseHTMLPage("https://example.org/start", []byte(`<html><body>
<a href="javascript:alert(1)">script</a>
<a href="https://example.org/next?utm_source=one">one</a>
<a href="https://example.org/next">two</a>
</body></html>`))
	if len(page.Links) != 1 || page.Links[0].URL != "https://example.org/next" {
		t.Fatalf("links = %#v", page.Links)
	}
}

func TestParseHTMLPageAppliesLateRobotsNofollowToEarlierLinks(t *testing.T) {
	page := ParseHTMLPage("https://example.org/start", []byte(`<html><body>
<a href="/next">Next</a><meta name="robots" content="nofollow">
</body></html>`))
	if !page.Nofollow || len(page.Links) != 1 || !page.Links[0].NoFollow {
		t.Fatalf("late robots directive result = %#v", page)
	}
}
