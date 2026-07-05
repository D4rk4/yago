package yacysearch

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func htmlSearchBody(t *testing.T, newTab bool) string {
	t.Helper()
	search := &fakeSearch{response: searchcore.Response{
		TotalResults: 1,
		Results:      []searchcore.Result{{Title: "Result", URL: "https://example.org/doc"}},
	}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"http://node.test/yacysearch.html?query=go",
		nil,
	)
	htmlEndpoint{
		search:      search,
		suggestions: newRecentQueries(),
		newTab:      newTab,
	}.ServeHTTP(
		rec,
		req,
	)

	return rec.Body.String()
}

func TestHTMLEndpointLinksDefaultToSameTab(t *testing.T) {
	body := htmlSearchBody(t, false)
	if strings.Contains(body, `target="_blank"`) {
		t.Fatal("same-tab default should not render target=_blank")
	}
	if !strings.Contains(body, `rel="noreferrer nofollow"`) {
		t.Fatal("same-tab links should keep referrer and follow hygiene")
	}
}

func TestHTMLEndpointLinksOpenNewTabWithIndicatorWhenEnabled(t *testing.T) {
	body := htmlSearchBody(t, true)
	for _, want := range []string{
		`target="_blank"`,
		`rel="noopener noreferrer nofollow"`,
		"(opens in new tab)",
		"↗",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("new-tab results missing %q in %s", want, body)
		}
	}
}

func TestHTMLEndpointHighlightsQueryTerms(t *testing.T) {
	search := &fakeSearch{response: searchcore.Response{
		TotalResults: 1,
		Results: []searchcore.Result{{
			Title:   "Result",
			URL:     "https://example.org/doc",
			Snippet: "Golang crawls <fast>",
		}},
	}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"http://node.test/yacysearch.html?query=golang",
		nil,
	)
	htmlEndpoint{search: search, suggestions: newRecentQueries()}.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "<mark>Golang</mark>") {
		t.Fatalf("missing highlighted term in %s", body)
	}
	if strings.Contains(body, "<fast>") {
		t.Fatal("snippet markup must stay escaped")
	}
}

func TestHTMLEndpointRendersAccessibleAutocomplete(t *testing.T) {
	body := htmlSearchBody(t, false)
	for _, want := range []string{
		`role="combobox"`,
		`aria-autocomplete="list"`,
		`aria-controls="ac-list"`,
		`role="listbox"`,
		`autocomplete="off"`,
		"/suggest.json?q=",
		"aria-activedescendant",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("search page missing %q", want)
		}
	}
}
