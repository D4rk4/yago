package yagonode

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestResultSizeName(t *testing.T) {
	if got := resultSizeName(0); got != "" {
		t.Fatalf("resultSizeName(0) = %q, want empty", got)
	}
	if got := resultSizeName(2048); got != "2048 bytes" {
		t.Fatalf("resultSizeName(2048) = %q", got)
	}
}

func TestPortalSourceHighlightsSnippetsAndCarriesMeta(t *testing.T) {
	inner := &fakeSearcher{resp: searchcore.Response{
		Request: searchcore.Request{Terms: []string{"golang"}},
		Results: []searchcore.Result{{
			Title:   "Go",
			URL:     "https://example.org/go",
			Snippet: "Golang crawls <fast>",
			Host:    "example.org",
			Date:    "20260701",
			Size:    2048,
		}, {
			Title:   "Unknown publication",
			URL:     "https://example.org/unknown",
			Snippet: "Golang publication unknown",
		}},
	}}

	results, err := newPortalSource(inner).Search(t.Context(), "golang", "", 0, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results.Results) != 2 || results.Results[1].Date != "" {
		t.Fatalf("unknown publication date = %#v", results.Results)
	}
	result := results.Results[0]
	if !strings.Contains(string(result.SnippetHTML), "<mark>Golang</mark>") ||
		strings.Contains(string(result.SnippetHTML), "<fast>") {
		t.Fatalf("SnippetHTML = %q", result.SnippetHTML)
	}
	if result.Host != "example.org" || result.Date != "Wed, 01 Jul 2026" ||
		result.SizeName != "2048 bytes" {
		t.Fatalf("meta = %#v", result)
	}
}

func TestAdminSearchSourceHighlightsSnippets(t *testing.T) {
	inner := &fakeSearcher{resp: searchcore.Response{
		Request: searchcore.Request{Terms: []string{"golang"}},
		Results: []searchcore.Result{{
			Title:   "Go",
			URL:     "https://example.org/go",
			Snippet: "Golang guide",
			Date:    "20260701",
			Size:    1024,
		}, {
			Title: "Unknown publication",
			URL:   "https://example.org/unknown",
			Date:  "00010101",
		}, {
			Title: "Malformed publication",
			URL:   "https://example.org/malformed",
			Date:  "July 2026",
		}},
	}}

	results, err := searchSource{searcher: inner}.Search(
		t.Context(),
		adminui.SearchQuery{Query: "golang", Global: true, Limit: 10},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	result := results.Results[0]
	if !strings.Contains(string(result.SnippetHTML), "<mark>Golang</mark>") ||
		result.SizeName != "1024 bytes" || result.Date != "Wed, 01 Jul 2026" {
		t.Fatalf("result = %#v", result)
	}
	if results.Results[1].Date != "" || results.Results[2].Date != "" {
		t.Fatalf("unknown publication dates = %#v", results.Results[1:])
	}
}
