package pageparse_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawler/internal/pageparse"
)

const sampleHTML = `<!DOCTYPE html>
<html lang="de">
<head><title>Sample &amp; Title</title><meta name="description" content=" Sample page description. "><style>.x{color:red}</style></head>
<body>
<script>var ignored = "noise";</script>
<h1>Primary Heading</h1>
<h2>Secondary Heading</h2>
<p>Hello indexable world.</p>
<a href="/local">local</a>
<a href="http://other.com/x">external</a>
</body></html>`

func TestParseHTMLExtractsFields(t *testing.T) {
	page := pageparse.ParseHTML("http://example.com/", "text/html", []byte(sampleHTML))

	if page.Title != "Sample & Title" {
		t.Errorf("title = %q", page.Title)
	}
	if page.Language != "de" {
		t.Errorf("language = %q", page.Language)
	}
	if page.Description != "Sample page description." {
		t.Errorf("description = %q", page.Description)
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
	if len(page.Headings) != 2 || page.Headings[0] != "Primary Heading" {
		t.Errorf("headings = %v", page.Headings)
	}
}

func TestParseHTMLUsesFirstNonEmptyMetaDescription(t *testing.T) {
	page := pageparse.ParseHTML(
		"https://example.com/page",
		"text/html",
		[]byte(`<html><head>
<meta name="description" content="   ">
<meta name="DESCRIPTION" content="First useful description.">
<meta name="description" content="Second description.">
</head></html>`),
	)

	if page.Description != "First useful description." {
		t.Fatalf("description = %q", page.Description)
	}
}

func TestParseHTMLSplitsNoFollowLinks(t *testing.T) {
	page := pageparse.ParseHTML(
		"https://example.com/page",
		"text/html",
		[]byte(`<html><body>
	<a href="/follow">follow</a>
	<a rel="UGC,nofollow sponsored" href="/blocked">blocked</a>
	<a href="">empty</a>
	</body></html>`),
	)

	if len(page.Links) != 2 {
		t.Fatalf("all links = %v", page.Links)
	}
	if len(page.FollowableLinks) != 1 || page.FollowableLinks[0] != "/follow" {
		t.Fatalf("followable links = %v", page.FollowableLinks)
	}
	if len(page.NoFollowLinks) != 1 || page.NoFollowLinks[0] != "/blocked" {
		t.Fatalf("nofollow links = %v", page.NoFollowLinks)
	}
	if len(page.OutboundAnchors) != 2 ||
		page.OutboundAnchors[0].TargetURL != "https://example.com/follow" ||
		page.OutboundAnchors[0].Text != "follow" ||
		!page.OutboundAnchors[1].NoFollow ||
		!page.OutboundAnchors[1].UserGenerated ||
		!page.OutboundAnchors[1].Sponsored {
		t.Fatalf("outbound anchors = %#v", page.OutboundAnchors)
	}
}

func TestParseHTMLBoundsAndNormalizesOutboundAnchors(t *testing.T) {
	longText := strings.Repeat("x", 300)
	page := pageparse.ParseHTML(
		"https://example.com/dir/page",
		"text/html",
		[]byte(`<html><body>
<a href="../text#part"> visible   text </a>
<a href="/aria" aria-label="Accessible label"></a>
<a href="/title" title="Title label"></a>
<a href="/long">`+longText+`</a>
<a href="mailto:editor@example.com">mail</a>
<a href="http:/missing-host">missing host</a>
</body></html>`),
	)

	if len(page.OutboundAnchors) != 4 {
		t.Fatalf("outbound anchors = %#v", page.OutboundAnchors)
	}
	if page.OutboundAnchors[0].TargetURL != "https://example.com/text" ||
		page.OutboundAnchors[0].Text != "visible text" ||
		page.OutboundAnchors[1].Text != "Accessible label" ||
		page.OutboundAnchors[2].Text != "Title label" ||
		len([]rune(page.OutboundAnchors[3].Text)) != 256 {
		t.Fatalf("outbound anchors = %#v", page.OutboundAnchors)
	}
}

func TestParseHTMLBoundsOutboundAnchorCardinality(t *testing.T) {
	var body strings.Builder
	body.WriteString("<html><body>")
	for index := 0; index < 1025; index++ {
		body.WriteString(`<a href="/target/`)
		body.WriteString(strconv.Itoa(index))
		body.WriteString(`">target</a>`)
	}
	body.WriteString("</body></html>")
	page := pageparse.ParseHTML("https://example.com/page", "text/html", []byte(body.String()))

	if len(page.Links) != 1025 || len(page.OutboundAnchors) != 1024 {
		t.Fatalf("links/anchors = %d/%d", len(page.Links), len(page.OutboundAnchors))
	}
}

func TestParseHTMLExtractsImageMetadata(t *testing.T) {
	longAlt := strings.Repeat("x", 180)
	page := pageparse.ParseHTML(
		"https://example.com/dir/page",
		"text/html",
		[]byte(`<html><body>
<img src="/img/cat.png#hero" alt=" Cat image ">
<img src="https://cdn.example.net/dog.webp" alt="`+longAlt+`">
<img src="data:image/png;base64,ignored" alt="ignored">
<img src="http:/missing-host.png" alt="ignored">
<img src="" alt="empty source">
</body></html>`),
	)

	if len(page.Images) != 2 {
		t.Fatalf("images = %#v", page.Images)
	}
	if page.Images[0].URL != "https://example.com/img/cat.png" ||
		page.Images[0].AltText != "Cat image" {
		t.Fatalf("first image = %#v", page.Images[0])
	}
	if page.Images[1].URL != "https://cdn.example.net/dog.webp" ||
		len([]rune(page.Images[1].AltText)) != 160 {
		t.Fatalf("second image = %#v", page.Images[1])
	}
}

func TestParseHTMLExtractsCanonicalURL(t *testing.T) {
	page := pageparse.ParseHTML(
		"https://example.com/dir/page?ref=1",
		"text/html",
		[]byte(`<html><head><link rel="canonical" href="/canonical#fragment"></head></html>`),
	)

	if page.CanonicalURL != "https://example.com/canonical" {
		t.Fatalf("canonical URL = %q", page.CanonicalURL)
	}
}

func TestParseHTMLUsesFirstValidCanonicalURL(t *testing.T) {
	page := pageparse.ParseHTML(
		"https://example.com/dir/page",
		"text/html",
		[]byte(`<html><head>
<link rel="canonical" href="mailto:editor@example.com">
<link rel="alternate canonical" href="../preferred">
</head></html>`),
	)

	if page.CanonicalURL != "https://example.com/preferred" {
		t.Fatalf("canonical URL = %q", page.CanonicalURL)
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
	page := pageparse.ParseHTML("http://example.com/", "text/html", []byte(articleHTML))

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

	page := pageparse.ParseHTML("http://example.com/", "text/html", body)

	if !strings.Contains(page.Text, "café") {
		t.Errorf("expected transcoded 'café', got %q", page.Text)
	}
}

func TestParseHTMLFallsBackOnBadCharset(t *testing.T) {
	page := pageparse.ParseHTML(
		"http://example.com/",
		"text/html; charset=does-not-exist",
		[]byte("<html><body>fallback text</body></html>"),
	)

	if !strings.Contains(page.Text, "fallback text") {
		t.Fatalf("fallback text missing: %q", page.Text)
	}
}

func TestParseHTMLKeepsTextOfChromeOnlyPages(t *testing.T) {
	// A page of bare controls still yields its visible text (the precision
	// extractor keeps the span, drops the button label as boilerplate); the
	// full-DOM fallback for a genuinely empty extraction is covered by the
	// internal seam test.
	page := pageparse.ParseHTML(
		"http://example.com/",
		"text/html",
		[]byte("<html><body><button>Login</button><span>Menu</span></body></html>"),
	)

	if !strings.Contains(page.Text, "Menu") {
		t.Fatalf("chrome-only page text missing: %q", page.Text)
	}
}
