package publicportal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type facetedSource struct{}

func (facetedSource) Search(context.Context, string, string, int, int) (SearchResults, error) {
	return SearchResults{
		Query:        "go",
		TotalResults: 1,
		Results:      []SearchResult{{Title: "Go", URL: "https://a.example/x"}},
		Facets: []FacetGroup{{
			Title: "Host", Scope: "the local corpus",
			Items: []FacetItem{
				{Label: "a.example", Count: 3, URL: "/?q=go+site%3Aa.example"},
				{Label: "plain.example", Count: 1},
			},
		}},
	}, nil
}

func TestPortalRendersFacetSidebar(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?q=go", nil)
	New(facetedSource{}, false).ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{
		`<aside class="facets" aria-label="Filter results">`,
		"<summary>Filters</summary>",
		"<fieldset><legend>Host — counts from the local corpus</legend>",
		"&#43;site%3Aa.example",
		`>a.example</a> <span class="count">(3)</span>`,
		`plain.example <span class="count">(1)</span>`,
		`class="serp-grid"`,
	} {
		if !strings.Contains(strings.ToLower(body), strings.ToLower(want)) {
			t.Fatalf("facet sidebar missing %q: %s", want, body)
		}
	}
}

func TestPortalHidesSidebarWithoutFacets(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?q=go", nil)
	New(cachedSource{}, false).ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), `class="serp-grid"`) {
		t.Fatal("sidebar grid rendered without facets")
	}
}
