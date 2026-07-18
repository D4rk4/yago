package pageparse

import "testing"

func TestParseHTMLReadsAuthorMetaTags(t *testing.T) {
	page := ParseHTML("https://a.example/x", "text/html", []byte(`<html><head>
<meta name="author" content=" Jane  Doe ">
</head><body>text</body></html>`))
	if page.Author != "Jane Doe" {
		t.Fatalf("author = %q, want collapsed Jane Doe", page.Author)
	}

	page = ParseHTML("https://a.example/y", "text/html", []byte(`<html><head>
<meta name="author" content="Fallback Name">
<meta property="article:author" content="Article Author">
</head><body>text</body></html>`))
	if page.Author != "Article Author" {
		t.Fatalf("author = %q, want article:author to win", page.Author)
	}

	page = ParseHTML("https://a.example/z", "text/html", []byte(`<html><head>
<meta name="author" content="   ">
</head><body>text</body></html>`))
	if page.Author != "" {
		t.Fatalf("author = %q, want empty for blank content", page.Author)
	}
}
