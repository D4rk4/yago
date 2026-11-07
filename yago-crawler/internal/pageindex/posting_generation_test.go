package pageindex_test

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/pageindex"
	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
	"github.com/D4rk4/yago/yagomodel"
)

func TestGeneratedComponentCountUsesJavaComponentCount(t *testing.T) {
	tokens := make([]string, 300)
	for index := range tokens {
		tokens[index] = "harbor"
	}
	postings := pageindex.BuildPostings(
		pageparse.ParsedPage{
			URL: "https://example.com/a//b", Language: "en", Text: strings.Join(tokens, " "),
		},
		pageparse.PageStats{},
	)
	if len(postings) != 1 {
		t.Fatalf("postings = %d", len(postings))
	}
	if got := postings[0].Properties[yagomodel.ColURLComponentCount]; got != "5" {
		t.Fatalf("component cardinal = %q, want 5", got)
	}
	if got := postings[0].Properties[yagomodel.ColHitCount]; got != "255" {
		t.Fatalf("hit cardinal = %q, want 255", got)
	}
}
