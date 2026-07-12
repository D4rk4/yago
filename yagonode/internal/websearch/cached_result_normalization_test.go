package websearch

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNormalizeResultsBoundsPayload(t *testing.T) {
	results := make([]Result, 0, maxCachedResults+2)
	results = append(results, Result{
		URL: "https://example.test/" + strings.Repeat("x", maxCachedURLBytes),
	})
	results = append(results, Result{
		Title:   strings.Repeat("ж", maxCachedTitleBytes/2+1),
		URL:     "https://example.test/long",
		Snippet: strings.Repeat("€", maxCachedSnippetBytes/3+1),
	})
	for index := range maxCachedResults {
		results = append(results, Result{
			Title:   "short",
			URL:     "https://example.test/page",
			Snippet: fmt.Sprintf("snippet-%d", index),
		})
	}

	normalized := normalizeResults(results, maxCachedResults)
	if len(normalized) != maxCachedResults {
		t.Fatalf("normalized results = %d, want %d", len(normalized), maxCachedResults)
	}
	if len(normalized[0].Title) > maxCachedTitleBytes ||
		len(normalized[0].Snippet) > maxCachedSnippetBytes {
		t.Fatalf(
			"normalized title/snippet bytes = %d/%d",
			len(normalized[0].Title),
			len(normalized[0].Snippet),
		)
	}
	if !utf8.ValidString(normalized[0].Title) || !utf8.ValidString(normalized[0].Snippet) {
		t.Fatal("normalized text must remain valid UTF-8")
	}
}
