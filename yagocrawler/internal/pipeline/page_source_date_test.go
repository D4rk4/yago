package pipeline

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawler/internal/pageparse"
)

func TestPageWithSourceDateUsesEvidencePriority(t *testing.T) {
	structured := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	httpDate := time.Date(2026, 7, 1, 0, 0, 0, 0, time.FixedZone("test", 3600))
	sitemapDate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	page := pageWithSourceDate(pageparse.ParsedPage{
		ModifiedAt: structured, DateConfidence: 1, DateSource: "json-ld",
	}, httpDate, sitemapDate)
	if page.ModifiedAt != structured || page.DateConfidence != 1 || page.DateSource != "json-ld" {
		t.Fatalf("structured page = %#v", page)
	}

	page = pageWithSourceDate(pageparse.ParsedPage{
		PublishedAt: structured, DateConfidence: 0.9, DateSource: "itemprop",
	}, httpDate, sitemapDate)
	if page.ModifiedAt != httpDate.UTC() || page.DateConfidence != httpDateConfidence ||
		page.DateSource != "itemprop+http-last-modified" {
		t.Fatalf("http page = %#v", page)
	}

	page = pageWithSourceDate(pageparse.ParsedPage{}, time.Time{}, sitemapDate)
	if page.ModifiedAt != sitemapDate || page.DateConfidence != sitemapDateConfidence ||
		page.DateSource != "sitemap-lastmod" {
		t.Fatalf("sitemap page = %#v", page)
	}
}

func TestPageWithSourceDateLeavesUnknownPageUnknown(t *testing.T) {
	page := pageWithSourceDate(pageparse.ParsedPage{}, time.Time{}, time.Time{})
	if !page.ModifiedAt.IsZero() || page.DateConfidence != 0 || page.DateSource != "" {
		t.Fatalf("unknown page = %#v", page)
	}
}

func TestPageWithSourceDateDoesNotDuplicateSource(t *testing.T) {
	httpDate := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	page := pageWithSourceDate(pageparse.ParsedPage{
		DateConfidence: 0.5, DateSource: "http-last-modified",
	}, httpDate, time.Time{})
	if page.DateConfidence != 0.5 || page.DateSource != "http-last-modified" {
		t.Fatalf("page = %#v", page)
	}
}
