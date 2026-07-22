package searchlocal

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestSearcherSeparatesSiteHostAndDomainLists(t *testing.T) {
	index := &fakeIndex{}
	_, err := NewSearcher(index).Search(t.Context(), searchcore.Request{
		Query:          "needle",
		SiteHost:       "www.site.example",
		IncludeDomains: []string{"parent.example"},
		ExcludeDomains: []string{"blocked.example"},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	request := index.got
	if request.SiteHost != "www.site.example" ||
		len(request.IncludeDomain) != 1 || request.IncludeDomain[0] != "parent.example" ||
		len(request.ExcludeDomain) != 1 || request.ExcludeDomain[0] != "blocked.example" {
		t.Fatalf("index request = %#v", request)
	}
}

func TestDomainListsReachSearchIndexSuffixMatcher(t *testing.T) {
	index, err := searchindex.NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	document := documentstore.Document{
		NormalizedURL: "https://docs.parent.example/guide",
		Title:         "Needle guide",
		ExtractedText: "Needle guide",
		Language:      "en",
	}
	if err := index.Index(t.Context(), document); err != nil {
		t.Fatalf("Index: %v", err)
	}
	response, err := NewSearcher(index).Search(t.Context(), searchcore.Request{
		Query:          "needle",
		Terms:          []string{"needle"},
		Limit:          10,
		IncludeDomains: []string{"parent.example"},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(response.Results) != 1 || response.Results[0].URL != document.NormalizedURL {
		t.Fatalf("results = %#v", response.Results)
	}
}
