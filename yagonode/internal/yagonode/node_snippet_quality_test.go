package yagonode

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/searchlocal"
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

func TestPortalSourceUsesAnalyzerQueryMatches(t *testing.T) {
	inner := &fakeSearcher{resp: searchcore.Response{
		Request: searchcore.Request{Terms: []string{"person"}},
		Results: []searchcore.Result{{
			Title:        "People",
			URL:          "https://example.org/people",
			Snippet:      "people",
			QueryMatches: []searchcore.QueryMatch{{Start: 0, End: 6}},
		}},
	}}

	results, err := newPortalSource(inner).Search(t.Context(), "person", "", 0, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := results.Results[0].SnippetHTML; got != "<mark>people</mark>" {
		t.Fatalf("SnippetHTML = %q", got)
	}
}

func TestPortalSourceHonorsAuthoritativeEmptyQueryMatches(t *testing.T) {
	inner := &fakeSearcher{resp: searchcore.Response{
		Request: searchcore.Request{Terms: []string{"space"}},
		Results: []searchcore.Result{{
			Title:        "Spacecraft",
			URL:          "https://example.org/spaceship",
			Snippet:      "spaceship",
			QueryMatches: []searchcore.QueryMatch{},
		}},
	}}

	results, err := newPortalSource(inner).Search(t.Context(), "space", "", 0, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := results.Results[0].SnippetHTML; got != "spaceship" {
		t.Fatalf("SnippetHTML = %q", got)
	}
}

func TestAdminSearchSourceUsesAnalyzerQueryMatches(t *testing.T) {
	inner := &fakeSearcher{resp: searchcore.Response{
		Request: searchcore.Request{Terms: []string{"person"}},
		Results: []searchcore.Result{{
			Title:        "People",
			URL:          "https://example.org/people",
			Snippet:      "people",
			QueryMatches: []searchcore.QueryMatch{{Start: 0, End: 6}},
		}},
	}}

	results, err := searchSource{searcher: inner}.Search(
		t.Context(),
		adminui.SearchQuery{Query: "person", Global: true, Limit: 10},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := results.Results[0].SnippetHTML; got != "<mark>people</mark>" {
		t.Fatalf("SnippetHTML = %q", got)
	}
}

func TestPortalSourceHighlightsRetrievedRussianInflections(t *testing.T) {
	index, err := searchindex.NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	if err := index.Index(t.Context(), documentstore.Document{
		NormalizedURL: "https://example.org/russian-morphology",
		Title:         "Правовой обзор",
		ExtractedText: "Чрезвычайных полномочий передали Путину.",
		Language:      "ru",
	}); err != nil {
		t.Fatalf("Index: %v", err)
	}

	results, err := newPortalSource(searchlocal.NewSearcher(index)).Search(
		t.Context(),
		"Чрезвычайные полномочия Путина",
		"",
		0,
		10,
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	want := "<mark>Чрезвычайных</mark> <mark>полномочий</mark> передали <mark>Путину</mark>."
	if len(results.Results) != 1 || string(results.Results[0].SnippetHTML) != want {
		t.Fatalf("results = %#v", results.Results)
	}
}
