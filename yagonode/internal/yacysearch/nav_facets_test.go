package yacysearch

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestHTMLRefineURLAppendsOperatorAndPreservesFilters(t *testing.T) {
	params := url.Values{
		"query":       {"go"},
		"resource":    {"local"},
		"contentdom":  {"text"},
		"nav":         {"all"},
		"count":       {"10"}, // alias — must be dropped
		"startRecord": {"30"}, // must reset to the first page on refine
	}

	got := htmlRefineURL("http://node.test/yacysearch.html", params, "go", "filetype:pdf")

	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse refine URL: %v", err)
	}
	q := parsed.Query()
	if q.Get("query") != "go filetype:pdf" {
		t.Fatalf("query = %q, want operator appended (%s)", q.Get("query"), got)
	}
	for key, want := range map[string]string{"resource": "local", "contentdom": "text", "nav": "all"} {
		if q.Get(key) != want {
			t.Fatalf("filter %q = %q, want %q (%s)", key, q.Get(key), want, got)
		}
	}
	if q.Get("startRecord") != "0" {
		t.Fatalf("refine should reset to first page, startRecord = %q", q.Get("startRecord"))
	}
	if q.Has("count") {
		t.Fatalf("count alias should be dropped: %s", got)
	}
}

func TestHTMLEndpointRendersNavigationFacets(t *testing.T) {
	search := &fakeSearch{response: searchcore.Response{
		TotalResults: 50,
		Results:      []searchcore.Result{{Title: "Result", URL: "https://go.dev/"}},
		Facets: []searchcore.FacetGroup{
			{
				Name: "host",
				Terms: []searchcore.FacetTerm{
					{Term: "go.dev", Count: 42},
					{Term: "example.org", Count: 7},
				},
			},
			{Name: "protocol", Terms: []searchcore.FacetTerm{{Term: "https", Count: 49}}},
		},
	}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"http://node.test/yacysearch.html?query=go&resource=local&nav=all&maximumRecords=10",
		nil,
	)

	htmlEndpoint{search: search, suggestions: newRecentQueries()}.ServeHTTP(rec, req)

	body := rec.Body.String()
	// Navigator headings and a linkable value with a filter-preserving refine URL.
	for _, want := range []string{
		"Provider", "Protocol", "go.dev", "(42)",
		"site%3Ago.dev", "resource=local", "startRecord=0",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("facet render missing %q:\n%s", want, body)
		}
	}
	// The protocol dimension has no query operator, so its value is a plain
	// labelled count rather than a refine link.
	if strings.Contains(body, ">https</a>") {
		t.Fatalf("protocol value must not be a refine link:\n%s", body)
	}
}
