package pageparse

import "testing"

func TestParseHTMLReadsKeywordsAndPublisherMetaTags(t *testing.T) {
	page := ParseHTML("https://a.example/x", "text/html", []byte(`<html><head>
<meta name="keywords" content="go, search, crawler">
<meta name="publisher" content=" Example  Press ">
</head><body>text</body></html>`))
	if page.Keywords != "go, search, crawler" {
		t.Fatalf("keywords = %q", page.Keywords)
	}
	if page.Publisher != "Example Press" {
		t.Fatalf("publisher = %q, want collapsed Example Press", page.Publisher)
	}

	// og:site_name is the publisher fallback when no name=publisher is present.
	page = ParseHTML("https://a.example/y", "text/html", []byte(`<html><head>
<meta property="og:site_name" content="OG Site">
</head><body>text</body></html>`))
	if page.Publisher != "OG Site" {
		t.Fatalf("publisher = %q, want og:site_name fallback", page.Publisher)
	}

	// A name=publisher tag wins over the og:site_name fallback.
	page = ParseHTML("https://a.example/z", "text/html", []byte(`<html><head>
<meta property="og:site_name" content="OG Site">
<meta name="publisher" content="Named Publisher">
</head><body>text</body></html>`))
	if page.Publisher != "Named Publisher" {
		t.Fatalf("publisher = %q, want name=publisher to win", page.Publisher)
	}

	// Blank or absent metadata leaves the fields empty.
	page = ParseHTML("https://a.example/w", "text/html", []byte(`<html><head>
<meta name="keywords" content="   ">
</head><body>text</body></html>`))
	if page.Keywords != "" || page.Publisher != "" {
		t.Fatalf("keywords=%q publisher=%q, want empty", page.Keywords, page.Publisher)
	}
}
