package yacysearch

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestHTMLEndpointLinksCachedCopyAndFormats(t *testing.T) {
	search := &fakeSearch{response: searchcore.Response{
		TotalResults: 4,
		Request:      searchcore.Request{Terms: []string{"go"}},
		Results: []searchcore.Result{
			// A local hit inside a global search carries the request source, so
			// the cached link must key off StoredLocally, not SourceLocal.
			{
				DocumentID: "https://a.example/x", Analyzer: "en",
				Title: "Local", URL: "https://a.example/x", Source: searchcore.SourceGlobal,
				BodyQueryMatches: []searchcore.QueryMatch{{Start: 10, End: 14}},
			},
			{Title: "Plain", URL: "https://d.example/plain", Source: searchcore.SourceGlobal},
			{Title: "Remote", URL: "https://b.example/y", Source: searchcore.SourceRemote},
			{Title: "External", URL: "https://c.example/z", Source: searchcore.SourceWeb},
		},
	}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"http://node.test/yacysearch.html?query=go",
		nil,
	)
	htmlEndpoint{search: search, suggestions: newRecentQueries()}.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(
		body,
		`href="/cached?analyzer=en&amp;end=14&amp;start=10&amp;terms=go&amp;u=https%3A%2F%2Fa.example%2Fx">cached`,
	) {
		t.Fatalf("local result missing cached link: %s", body)
	}
	if !strings.Contains(body, `href="/cached?u=https%3A%2F%2Fd.example%2Fplain">cached`) {
		t.Fatalf("plain local result missing cached link: %s", body)
	}
	if strings.Contains(body, "cached?u=https%3A%2F%2Fb.example%2Fy") {
		t.Fatal("remote result must not link a cached copy")
	}
	if !strings.Contains(body, "[peer]") {
		t.Fatalf("peer result missing provenance label: %s", body)
	}
	if !strings.Contains(body, "[web]") {
		t.Fatalf("web-fallback result missing provenance label: %s", body)
	}
	if strings.Contains(body, "[ddgs]") {
		t.Fatalf("web-fallback result carries its internal provider name: %s", body)
	}
	if !strings.Contains(body, "/yacysearch.json") || !strings.Contains(body, ">JSON</a>") {
		t.Fatalf("visible JSON format link missing: %s", body)
	}
	if !strings.Contains(body, ">RSS</a>") {
		t.Fatalf("visible RSS format link missing: %s", body)
	}
	for _, want := range []string{
		`href="#results"`, "Skip to results",
		`<section id="results" tabindex="-1">`, `<p role="status">`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("a11y landmark missing %q: %s", want, body)
		}
	}
}
