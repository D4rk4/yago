package yacysearch

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestHTMLPageURLPreservesFiltersAndOverridesWindow(t *testing.T) {
	params := url.Values{
		"query":       {"go"},
		"resource":    {"local"},
		"contentdom":  {"text"},
		"author":      {"bob"},
		"language":    {"en"},
		"verify":      {"true"},
		"prefer":      {"example.org"},
		"count":       {"10"}, // OpenSearch alias — must yield to maximumRecords
		"startRecord": {"10"}, // current window — must be overridden
	}

	got := htmlPageURL("http://node.test/yacysearch.html", params, 20, 40)

	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse pager URL: %v", err)
	}
	q := parsed.Query()
	for key, want := range map[string]string{
		"query": "go", "resource": "local", "contentdom": "text",
		"author": "bob", "language": "en", "verify": "true", "prefer": "example.org",
	} {
		if q.Get(key) != want {
			t.Fatalf("filter %q = %q, want %q (%s)", key, q.Get(key), want, got)
		}
	}
	if q.Get("startRecord") != "40" || q.Get("maximumRecords") != "20" {
		t.Fatalf("page window not overridden: %s", got)
	}
	if q.Has("count") {
		t.Fatalf("count alias should be dropped in favor of maximumRecords: %s", got)
	}

	// The shared params map must not be mutated across pager links.
	if params.Get("startRecord") != "10" || !params.Has("count") {
		t.Fatalf("htmlPageURL mutated its input params: %v", params)
	}
}

func TestHTMLEndpointPagerPreservesFilters(t *testing.T) {
	search := &fakeSearch{response: searchcore.Response{
		TotalResults: 100,
		Results:      []searchcore.Result{{Title: "Result", URL: "https://example.org/doc"}},
	}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"http://node.test/yacysearch.html?query=go&resource=local&contentdom=text"+
			"&author=bob&language=en&verify=true&maximumRecords=10&startRecord=10",
		nil,
	)

	htmlEndpoint{search: search, suggestions: newRecentQueries()}.ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{
		"author=bob", "language=en", "verify=true", "contentdom=text", "resource=local",
		"query=go", "startRecord=0", "startRecord=20", "maximumRecords=10",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("pager dropped %q from its links:\n%s", want, body)
		}
	}
}
