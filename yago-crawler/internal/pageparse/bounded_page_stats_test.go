package pageparse_test

import (
	"slices"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
)

func TestBoundedPageStatsCountsOnlyResolvedLinks(t *testing.T) {
	stats := pageparse.BuildBoundedPageStats(
		pageparse.ParsedPage{
			URL: "https://example.org/page",
			Links: []string{
				"ftp://example.org/rejected",
				"/local",
				"https://external.example/page",
				"/beyond-limit",
			},
		},
		2,
	)
	if !slices.Equal(stats.LocalLinks, []string{"https://example.org/local"}) {
		t.Fatalf("local links = %v", stats.LocalLinks)
	}
	if !slices.Equal(
		stats.ExternalLinks,
		[]string{"https://external.example/page"},
	) {
		t.Fatalf("external links = %v", stats.ExternalLinks)
	}
}
