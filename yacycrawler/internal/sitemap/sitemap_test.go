package sitemap_test

import (
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacycrawler/internal/sitemap"
)

func TestParseXMLURLSet(t *testing.T) {
	doc, err := sitemap.ParseXML([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc> https://example.org/a </loc><lastmod>2026-07-01</lastmod></url>
  <url><loc>https://example.org/b</loc><lastmod>2026-07-02T10:11:12Z</lastmod></url>
  <url><loc>   </loc></url>
</urlset>`), 10)
	if err != nil {
		t.Fatalf("ParseXML: %v", err)
	}
	if len(doc.URLs) != 2 || len(doc.Sitemaps) != 0 || doc.Truncated {
		t.Fatalf("document = %#v", doc)
	}
	if doc.URLs[0].URL != "https://example.org/a" {
		t.Fatalf("first URL = %#v", doc.URLs[0])
	}
	if doc.URLs[0].LastModified != time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("first lastmod = %v", doc.URLs[0].LastModified)
	}
	if doc.URLs[1].LastModified != time.Date(2026, 7, 2, 10, 11, 12, 0, time.UTC) {
		t.Fatalf("second lastmod = %v", doc.URLs[1].LastModified)
	}
}

func TestParseXMLSitemapIndex(t *testing.T) {
	doc, err := sitemap.ParseXML([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap><loc>https://example.org/sitemap-a.xml</loc><lastmod>2026-07-01</lastmod></sitemap>
  <sitemap><loc>https://example.org/sitemap-b.xml</loc></sitemap>
</sitemapindex>`), 1)
	if err != nil {
		t.Fatalf("ParseXML: %v", err)
	}
	if len(doc.Sitemaps) != 1 || !doc.Truncated {
		t.Fatalf("document = %#v", doc)
	}
	if doc.Sitemaps[0].URL != "https://example.org/sitemap-a.xml" {
		t.Fatalf("sitemap = %#v", doc.Sitemaps[0])
	}
}

func TestParseXMLRejectsInvalidAndUnsupportedXML(t *testing.T) {
	for _, raw := range []string{"{", "<rss></rss>"} {
		if _, err := sitemap.ParseXML([]byte(raw), 10); err == nil {
			t.Fatalf("ParseXML(%q) should fail", raw)
		}
	}
}

func TestParseSitelist(t *testing.T) {
	doc := sitemap.ParseSitelist([]byte(`
# comment
https://example.org/a

https://example.org/b
https://example.org/c
`), 2)
	if len(doc.URLs) != 2 || !doc.Truncated {
		t.Fatalf("document = %#v", doc)
	}
	if doc.URLs[0].URL != "https://example.org/a" || doc.URLs[1].URL != "https://example.org/b" {
		t.Fatalf("urls = %#v", doc.URLs)
	}
}

func TestParseSitelistZeroLimit(t *testing.T) {
	doc := sitemap.ParseSitelist([]byte("https://example.org/a\n"), 0)
	if len(doc.URLs) != 0 || !doc.Truncated {
		t.Fatalf("document = %#v", doc)
	}
}

func TestParseXMLCapsHugeSitemap(t *testing.T) {
	var raw strings.Builder
	raw.WriteString(`<urlset>`)
	for i := 0; i < 4; i++ {
		raw.WriteString(`<url><loc>https://example.org/page</loc></url>`)
	}
	raw.WriteString(`</urlset>`)

	doc, err := sitemap.ParseXML([]byte(raw.String()), 3)
	if err != nil {
		t.Fatalf("ParseXML: %v", err)
	}
	if len(doc.URLs) != 3 || !doc.Truncated {
		t.Fatalf("document = %#v", doc)
	}
}
