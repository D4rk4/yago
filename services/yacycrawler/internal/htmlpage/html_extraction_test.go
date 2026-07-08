package htmlpage_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/htmlpage"
)

const article = `<!DOCTYPE html><html lang="en"><head><title>Sample Article</title></head>
<body><article><h1>Sample Article</h1>
<p>` + longText + `</p>
<p>` + longText + `</p>
<a href="/local/page">local</a>
<a href="http://other.example/ext">external</a>
</article></body></html>`

const longText = "The quick brown fox jumps over the lazy dog while the industrious " +
	"beaver builds a sturdy dam across the wide and winding river near the old mill town."

func TestExtractArticle(t *testing.T) {
	documents, err := htmlpage.New().
		Extract("http://host.example/dir/p", "text/html", []byte(article))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(documents) != 1 {
		t.Fatalf("want one document, got %d", len(documents))
	}
	doc := documents[0]
	if doc.Title != "Sample Article" {
		t.Fatalf("title = %q", doc.Title)
	}
	if !strings.Contains(doc.Text, "quick brown fox") {
		t.Fatalf("text missing body: %q", doc.Text)
	}
	if doc.Language != "en" {
		t.Fatalf("language = %q, want en", doc.Language)
	}
	if doc.LocalLinkCount != 1 || doc.ExternalLinkCount != 1 {
		t.Fatalf("links local=%d external=%d", doc.LocalLinkCount, doc.ExternalLinkCount)
	}
}

func TestExtractHonorsMetaRobots(t *testing.T) {
	page := `<!DOCTYPE html><html lang="en"><head><title>T</title>
<meta name="robots" content="noindex,nofollow"></head>
<body><article><p>` + longText + `</p><p>` + longText + `</p></article></body></html>`
	documents, err := htmlpage.New().Extract("http://host.example/p", "text/html", []byte(page))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !documents[0].RefusesIndexing || !documents[0].RefusesLinkDiscovery {
		t.Fatalf("meta robots not honored: %+v", documents[0])
	}
}

func TestExtractEmptyContentUnextractable(t *testing.T) {
	_, err := htmlpage.New().Extract("http://host.example/p", "text/html",
		[]byte("<html><body></body></html>"))
	if !errors.Is(err, crawlcapability.ErrUnextractable) {
		t.Fatalf("want ErrUnextractable, got %v", err)
	}
}

func TestMediaTypesDeclared(t *testing.T) {
	got := htmlpage.New().MediaTypes()
	if len(got) != 2 || got[0] != "text/html" {
		t.Fatalf("unexpected media types: %v", got)
	}
}
