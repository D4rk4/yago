package yacysearch

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagoproto"
)

func TestSuggestEndpointMergesLocalIndexTitles(t *testing.T) {
	recent := newRecentQueries()
	recent.Record("linux mint download")
	endpoint := suggestEndpoint{
		index:       indexSuggester{search: titledSearch("Linux kernel newbies")},
		suggestions: recent,
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yagoproto.PathSuggestJSON+"?q=linux",
		nil,
	)
	endpoint.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "Linux kernel newbies") {
		t.Fatalf("body %q missing the local index title", body)
	}
	if !strings.Contains(body, "linux mint download") {
		t.Fatalf("body %q missing the recent-query completion", body)
	}
	if strings.Index(body, "Linux kernel newbies") > strings.Index(body, "linux mint download") {
		t.Fatalf("body %q must list the index title before the recent query", body)
	}
}

func TestSuggestXMLEndpointMergesLocalIndexTitles(t *testing.T) {
	endpoint := suggestXMLEndpoint{
		index:       indexSuggester{search: titledSearch("Debian stable release notes")},
		suggestions: newRecentQueries(),
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yagoproto.PathSuggestXML+"?q=debian",
		nil,
	)
	endpoint.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "<Text>Debian stable release notes</Text>") {
		t.Fatalf("xml body %q missing the local index title item", rec.Body.String())
	}
}
