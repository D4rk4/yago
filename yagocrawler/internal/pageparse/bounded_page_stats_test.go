package pageparse_test

import (
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagocrawler/internal/pageparse"
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
		0,
		0,
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

func TestBoundedPageStatsStopsAtTokenTransition(t *testing.T) {
	for _, test := range []struct {
		name string
		text string
		want string
	}{
		{name: "decimal unit", text: "4.7Ohm trailing", want: "4.7"},
		{name: "punctuation", text: "word.next trailing", want: "word"},
		{name: "punctuated decimal", text: "4.7! trailing", want: "4.7!"},
		{name: "numeric unit", text: "42kg trailing", want: "42"},
	} {
		t.Run(test.name, func(t *testing.T) {
			stats := pageparse.BuildBoundedPageStats(
				pageparse.ParsedPage{Text: test.text},
				1,
				0,
				0,
			)
			if !slices.Equal(stats.Tokens, []string{test.want}) {
				t.Fatalf("tokens = %q, want [%q]", stats.Tokens, test.want)
			}
		})
	}
}
