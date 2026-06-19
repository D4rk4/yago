package yacycrawler_test

import (
	"strings"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler"
)

const sampleHTML = `<!DOCTYPE html>
<html lang="de">
<head><title>Sample &amp; Title</title><style>.x{color:red}</style></head>
<body>
<script>var ignored = "noise";</script>
<p>Hello indexable world.</p>
<a href="/local">local</a>
<a href="http://other.com/x">external</a>
</body></html>`

func TestParseHTMLExtractsFields(t *testing.T) {
	page := yacycrawler.ParseHTML("http://example.com/", "text/html", []byte(sampleHTML))

	if page.Title != "Sample & Title" {
		t.Errorf("title = %q", page.Title)
	}
	if page.Language != "de" {
		t.Errorf("language = %q", page.Language)
	}
	if strings.Contains(page.Text, "ignored") || strings.Contains(page.Text, "color") {
		t.Errorf("text should drop script/style: %q", page.Text)
	}
	if !strings.Contains(page.Text, "indexable world") {
		t.Errorf("text missing body: %q", page.Text)
	}
	if len(page.Links) != 2 {
		t.Errorf("links = %v", page.Links)
	}
}

const articleHTML = `<!DOCTYPE html>
<html lang="en">
<head><title>Real Article</title></head>
<body>
<nav>Home About Contact Login Subscribe now for our newsletter today</nav>
<header>Sitewide promotional banner advertisement buy now discount sale</header>
<article>
<h1>The Migratory Patterns of Arctic Terns</h1>
<p>The Arctic tern undertakes the longest migration of any known animal, travelling
roughly seventy thousand kilometres each year between its Arctic breeding grounds and
the Antarctic where it spends the winter. This remarkable journey means the bird sees
two summers annually and more daylight than any other creature on the planet.</p>
<p>Researchers tracked individual terns using tiny geolocators to reveal that the birds
follow a zig-zag route across the Atlantic ocean rather than a straight line, exploiting
prevailing wind systems to conserve energy over the enormous distance they cover.</p>
</article>
<footer>Copyright notice terms of service privacy policy cookie consent all rights reserved</footer>
</body></html>`

func TestParseHTMLExtractsMainContent(t *testing.T) {
	page := yacycrawler.ParseHTML("http://example.com/", "text/html", []byte(articleHTML))

	if !strings.Contains(page.Text, "Arctic tern undertakes the longest migration") {
		t.Errorf("main content missing: %q", page.Text)
	}
	for _, boilerplate := range []string{"Subscribe now", "promotional banner", "privacy policy"} {
		if strings.Contains(page.Text, boilerplate) {
			t.Errorf("boilerplate %q not stripped: %q", boilerplate, page.Text)
		}
	}
}

func TestParseHTMLTranscodesCharset(t *testing.T) {
	body := []byte("<html><head><meta charset=\"windows-1252\"></head><body>caf\xe9</body></html>")

	page := yacycrawler.ParseHTML("http://example.com/", "text/html", body)

	if !strings.Contains(page.Text, "café") {
		t.Errorf("expected transcoded 'café', got %q", page.Text)
	}
}
