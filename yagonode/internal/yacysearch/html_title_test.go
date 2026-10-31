package yacysearch

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestResponseHTMLItemsNormalizeMatchingTitleArrayDescription(t *testing.T) {
	snippet := `["Sample page title"]`
	matchStart := strings.Index(snippet, "page")
	items := responseHTMLItems([]searchcore.Result{{
		Title:        `["Sample page title", "Alternate title"]`,
		Snippet:      snippet,
		QueryMatches: []searchcore.QueryMatch{{Start: matchStart, End: matchStart + len("page")}},
		URL:          "https://example.org/imported",
	}}, []string{"page"}, 0)

	if len(items) != 1 || items[0].Title != "Sample page title" ||
		string(items[0].Description) != "Sample <mark>page</mark> title" {
		t.Fatalf("HTML result = %#v, want clean title and matching description", items)
	}
}
