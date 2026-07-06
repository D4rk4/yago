package searchindex

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func facetDoc(url, lang, author string, fetched time.Time) documentstore.Document {
	return documentstore.Document{
		NormalizedURL: url,
		Language:      lang,
		Metadata:      map[string]string{"author": author},
		FetchedAt:     fetched,
	}
}

func TestFacetCollectorGroupsAndOrder(t *testing.T) {
	when := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	collector := newFacetCollector(true)
	collector.observe(facetDoc("https://a.example/doc.pdf", "en", "Jane", when))
	collector.observe(facetDoc("https://a.example/page.html", "en", "Jane", when))
	collector.observe(facetDoc("http://b.example/x?q=1", "ru", "", time.Time{}))

	groups := collector.groups()
	byName := map[string]FacetGroup{}
	for _, group := range groups {
		byName[group.Name] = group
	}
	if got := byName["host"].Terms; len(got) != 2 || got[0].Term != "a.example" ||
		got[0].Count != 2 {
		t.Fatalf("host facet = %+v", got)
	}
	if got := byName["filetype"].Terms; len(got) != 2 ||
		got[0].Term != "html" && got[0].Term != "pdf" {
		t.Fatalf("filetype facet = %+v", got)
	}
	if got := byName["language"].Terms; got[0].Term != "en" || got[0].Count != 2 {
		t.Fatalf("language facet = %+v", got)
	}
	if got := byName["author"].Terms; len(got) != 1 || got[0].Term != "Jane" {
		t.Fatalf("author facet = %+v (empty author must be dropped)", got)
	}
	if got := byName["protocol"].Terms; len(got) != 2 {
		t.Fatalf("protocol facet = %+v", got)
	}
	if got := byName["month"].Terms; len(got) != 1 || got[0].Term != "2026-05" {
		t.Fatalf("month facet = %+v (zero time must be dropped)", got)
	}
}

func TestFacetCollectorNilAndLimits(t *testing.T) {
	if collector := newFacetCollector(false); collector != nil {
		t.Fatal("disabled collector must be nil")
	}
	var nilCollector *facetCollector
	nilCollector.observe(documentstore.Document{})
	if got := nilCollector.groups(); got != nil {
		t.Fatalf("nil collector groups = %+v", got)
	}

	collector := newFacetCollector(true)
	for i := 0; i < facetTermLimit+3; i++ {
		collector.observe(facetDoc(
			"https://host"+string(rune('a'+i))+".example/x", "", "", time.Time{},
		))
	}
	groups := collector.groups()
	for _, group := range groups {
		if group.Name == "host" && len(group.Terms) != facetTermLimit {
			t.Fatalf("host terms = %d, want capped at %d", len(group.Terms), facetTermLimit)
		}
	}
}

func TestFacetFieldHelpers(t *testing.T) {
	if got := documentFileType(documentstore.Document{
		NormalizedURL: "https://a.example/deep/file.verylongext",
	}); got != "" {
		t.Fatalf("overlong extension = %q, want dropped", got)
	}
	if got := documentProtocol(documentstore.Document{NormalizedURL: "no-scheme"}); got != "" {
		t.Fatalf("protocol without scheme = %q", got)
	}
	if got := urlPathOf("https://a.example"); got != "" {
		t.Fatalf("path of bare host = %q", got)
	}
	if got := urlPathOf("no-scheme-at-all"); got != "" {
		t.Fatalf("path without scheme = %q", got)
	}
	if got := urlPathOf("https://a.example/p/q.pdf?x=1"); got != "p/q.pdf" {
		t.Fatalf("path = %q", got)
	}
}

func TestSearchReturnsFacetsWhenRequested(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{{
			NormalizedURL: "https://a.example/golang.html",
			Title:         "Golang",
			ExtractedText: "golang text",
			Language:      "en",
		}},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	result, err := index.Search(t.Context(), SearchRequest{
		Query: "golang", MaxResults: 5, WithFacets: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Facets) == 0 {
		t.Fatalf("facets missing: %+v", result)
	}
}
