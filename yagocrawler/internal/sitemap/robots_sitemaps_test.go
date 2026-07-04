package sitemap_test

import (
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagocrawler/internal/sitemap"
)

func TestParseRobotsSitemapsExtractsDirectives(t *testing.T) {
	raw := []byte("User-agent: *\n" +
		"Disallow: /private\n" +
		"Sitemap: https://example.org/sitemap.xml\n" +
		"sitemap:   https://example.org/news.xml   # weekly news\n" +
		"SITEMAP: https://example.org/images.xml\n")

	got := sitemap.ParseRobotsSitemaps(raw, -1)

	want := []string{
		"https://example.org/sitemap.xml",
		"https://example.org/news.xml",
		"https://example.org/images.xml",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sitemaps = %#v, want %#v", got, want)
	}
}

func TestParseRobotsSitemapsIgnoresNonDirectives(t *testing.T) {
	raw := []byte("# Sitemap: https://commented.example.org/skip.xml\n" +
		"Sitemapindex: not-a-directive\n" +
		"Sitemap:\n" +
		"Sitemap:    \n" +
		"\n" +
		"Allow: /\n")

	if got := sitemap.ParseRobotsSitemaps(raw, -1); got != nil {
		t.Fatalf("sitemaps = %#v, want none", got)
	}
}

func TestParseRobotsSitemapsAppliesLimit(t *testing.T) {
	raw := []byte("Sitemap: https://example.org/1.xml\n" +
		"Sitemap: https://example.org/2.xml\n" +
		"Sitemap: https://example.org/3.xml\n")

	got := sitemap.ParseRobotsSitemaps(raw, 2)

	want := []string{
		"https://example.org/1.xml",
		"https://example.org/2.xml",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sitemaps = %#v, want %#v", got, want)
	}
}
