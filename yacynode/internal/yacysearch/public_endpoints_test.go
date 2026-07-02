package yacysearch

import (
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yacynode/internal/searchcore"
	"github.com/D4rk4/yago/yacyproto"
)

func TestMountRegistersPublicSearchEndpoints(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, &fakeSearch{})

	for _, path := range []string{
		yacyproto.PathYaCySearchJSON,
		yacyproto.PathYaCySearchRSS,
		yacyproto.PathYaCySearchHTML,
		yacyproto.PathOpenSearch,
		yacyproto.PathSuggestJSON,
		yacyproto.PathSuggestXML,
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestRSSEndpointReturnsOpenSearchShape(t *testing.T) {
	suggestions := newRecentQueries()
	search := &fakeSearch{response: searchcore.Response{
		TotalResults: 1,
		Results: []searchcore.Result{{
			Title:      "Result",
			URL:        "https://example.org/doc?x=1&y=2",
			Snippet:    "Result snippet",
			Host:       "example.org",
			Path:       "/doc",
			File:       "doc",
			URLHash:    "AAAAAAAAAAAA",
			Size:       12,
			Date:       "20260101",
			DisplayURL: "example.org/doc",
		}},
	}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"http://node.test/yacysearch.rss?query=golang&maximumRecords=5&resource=local&contentdom=text",
		nil,
	)

	rssEndpoint{search: search, suggestions: suggestions}.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/rss+xml; charset=utf-8" {
		t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	for _, expected := range []string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<rss version="2.0"`,
		`xmlns:yacy="http://www.yacy.net/"`,
		`<opensearch:itemsPerPage>5</opensearch:itemsPerPage>`,
		`<opensearch:totalResults>1</opensearch:totalResults>`,
		`<guid isPermaLink="false">AAAAAAAAAAAA</guid>`,
		`https://example.org/doc?x=1&amp;y=2`,
		`Thu, 01 Jan 2026 00:00:00 +0000`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in %s", expected, body)
		}
	}
	if got := suggestions.Suggest("go", 1); len(got) != 1 || got[0] != "golang" {
		t.Fatalf("suggestions = %#v", got)
	}
	if search.got.Limit != 5 || search.got.Source != searchcore.SourceLocal {
		t.Fatalf("request = %#v", search.got)
	}
}

func TestRSSEndpointRejectsInvalidRequests(t *testing.T) {
	for _, item := range []struct {
		method string
		target string
		search *fakeSearch
		code   int
	}{
		{method: http.MethodPost, target: yacyproto.PathYaCySearchRSS, search: &fakeSearch{}, code: http.StatusMethodNotAllowed},
		{method: http.MethodGet, target: yacyproto.PathYaCySearchRSS + "?maximumRecords=bad", search: &fakeSearch{}, code: http.StatusBadRequest},
		{method: http.MethodGet, target: yacyproto.PathYaCySearchRSS, search: &fakeSearch{err: errors.New("boom")}, code: http.StatusInternalServerError},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), item.method, item.target, nil)
		rssEndpoint{search: item.search, suggestions: newRecentQueries()}.ServeHTTP(rec, req)
		if rec.Code != item.code {
			t.Fatalf("%s %s: status = %d, want %d", item.method, item.target, rec.Code, item.code)
		}
	}
}

func TestOpenSearchDescriptionUsesRequestBaseURL(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"https://node.test/opensearchdescription.xml",
		nil,
	)
	req.TLS = &tls.ConnectionState{}

	openSearchEndpoint{}.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/opensearchdescription+xml; charset=utf-8" {
		t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	for _, expected := range []string{
		`<OpenSearchDescription xmlns="http://a9.com/-/spec/opensearch/1.1/"`,
		`https://node.test/yacysearch.html?query={searchTerms}&amp;startRecord={startIndex?}&amp;maximumRecords={count?}`,
		`https://node.test/yacysearch.rss?query={searchTerms}&amp;startRecord={startIndex?}&amp;maximumRecords={count?}`,
		`https://node.test/suggest.json?query={searchTerms}`,
		`https://node.test/suggest.xml?query={searchTerms}`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in %s", expected, body)
		}
	}
}

func TestOpenSearchDescriptionRejectsNonGET(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yacyproto.PathOpenSearch,
		nil,
	)

	openSearchEndpoint{}.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("status = %d allow=%q", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestSuggestEndpointReturnsRecentQueryMatches(t *testing.T) {
	suggestions := newRecentQueries()
	suggestions.Record("golang yacy")
	suggestions.Record("go dht")
	suggestions.Record("java yacy")

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yacyproto.PathSuggestJSON+"?q=go",
		nil,
	)

	suggestEndpoint{suggestions: suggestions}.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/x-suggestions+json" {
		t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
	}
	var got []json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode suggestions: %v", err)
	}
	var query string
	var values []string
	if err := json.Unmarshal(got[0], &query); err != nil {
		t.Fatalf("decode query: %v", err)
	}
	if err := json.Unmarshal(got[1], &values); err != nil {
		t.Fatalf("decode values: %v", err)
	}
	if query != "go" || len(values) != 2 || values[0] != "go dht" || values[1] != "golang yacy" {
		t.Fatalf("query=%q values=%#v", query, values)
	}
}

func TestSuggestEndpointAcceptsQueryParameterAndRejectsNonGET(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yacyproto.PathSuggestJSON+"?query=go",
		nil,
	)
	suggestEndpoint{suggestions: nil}.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `["go",null]`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yacyproto.PathSuggestJSON,
		nil,
	)
	suggestEndpoint{suggestions: newRecentQueries()}.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("status=%d allow=%q", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestSuggestXMLEndpointReturnsYaCySuggestionShape(t *testing.T) {
	suggestions := newRecentQueries()
	suggestions.Record("golang <yacy>")
	suggestions.Record("go dht")
	suggestions.Record("java yacy")

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yacyproto.PathSuggestXML+"?q=go",
		nil,
	)

	suggestXMLEndpoint{suggestions: suggestions}.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/x-suggestions+xml; charset=utf-8" {
		t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
	}
	var got searchSuggestionXML
	if err := xml.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode suggestions: %v", err)
	}
	values := suggestionTexts(got.Section.Items)
	if got.Query != "go" ||
		got.XMLNS != searchSuggestionNamespace ||
		len(values) != 2 ||
		values[0] != "go dht" ||
		values[1] != "golang <yacy>" {
		t.Fatalf("suggestions = %#v values=%#v", got, values)
	}
	if !strings.Contains(rec.Body.String(), "golang &lt;yacy&gt;") {
		t.Fatalf("body was not escaped: %s", rec.Body.String())
	}
}

func TestSuggestXMLEndpointAcceptsQueryParameterAndRejectsNonGET(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yacyproto.PathSuggestXML+"?query=go",
		nil,
	)
	suggestXMLEndpoint{suggestions: nil}.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<Query>go</Query>") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yacyproto.PathSuggestXML,
		nil,
	)
	suggestXMLEndpoint{suggestions: newRecentQueries()}.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("status=%d allow=%q", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestHTMLEndpointReturnsSearchPage(t *testing.T) {
	suggestions := newRecentQueries()
	search := &fakeSearch{response: searchcore.Response{
		TotalResults: 1,
		Results: []searchcore.Result{{
			Title:      "Result",
			URL:        "https://example.org/doc",
			Snippet:    "Result snippet",
			DisplayURL: "example.org/doc",
			Size:       12,
			Date:       "20260101",
		}},
		PartialFailures: []searchcore.PartialFailure{{Source: "peer", Reason: "timeout"}},
	}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"http://node.test/yacysearch.html?query=golang&resource=global&contentdom=text",
		nil,
	)

	htmlEndpoint{search: search, suggestions: suggestions}.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	for _, expected := range []string{
		`<title>Search for golang</title>`,
		`href="http://node.test/opensearchdescription.xml"`,
		`href="http://node.test/yacysearch.rss?query=golang&amp;resource=global&amp;contentdom=text"`,
		`peer: timeout`,
		`Result snippet`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in %s", expected, body)
		}
	}
	if got := suggestions.Suggest("go", 1); len(got) != 1 || got[0] != "golang" {
		t.Fatalf("suggestions = %#v", got)
	}
}

func suggestionTexts(items []suggestionItem) []string {
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, item.Text)
	}

	return values
}

func TestHTMLEndpointRejectsInvalidRequests(t *testing.T) {
	for _, item := range []struct {
		method string
		target string
		search *fakeSearch
		code   int
	}{
		{method: http.MethodPost, target: yacyproto.PathYaCySearchHTML, search: &fakeSearch{}, code: http.StatusMethodNotAllowed},
		{method: http.MethodGet, target: yacyproto.PathYaCySearchHTML + "?maximumRecords=bad", search: &fakeSearch{}, code: http.StatusBadRequest},
		{method: http.MethodGet, target: yacyproto.PathYaCySearchHTML, search: &fakeSearch{err: errors.New("boom")}, code: http.StatusInternalServerError},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), item.method, item.target, nil)
		htmlEndpoint{search: item.search, suggestions: newRecentQueries()}.ServeHTTP(rec, req)
		if rec.Code != item.code {
			t.Fatalf("%s %s: status = %d, want %d", item.method, item.target, rec.Code, item.code)
		}
	}
}

func TestRecentQueriesFilterDeduplicateAndCap(t *testing.T) {
	var nilQueries *recentQueries
	nilQueries.Record("ignored")
	if got := nilQueries.Suggest("i", 1); got != nil {
		t.Fatalf("nil suggestions = %#v", got)
	}

	queries := newRecentQueries()
	queries.Record(" ")
	if got := queries.Suggest("", 1); got != nil {
		t.Fatalf("empty prefix suggestions = %#v", got)
	}
	queries.Record("Golang")
	queries.Record("golang")
	queries.Record("java")
	for i := 0; i < recentQueryLimit+2; i++ {
		queries.Record("go item " + strconv.Itoa(i))
	}

	got := queries.Suggest("go", 0)
	if len(got) != publicSuggestionLimit || got[0] == "java" {
		t.Fatalf("suggestions = %#v", got)
	}
	if got := queries.Suggest("missing", 2); len(got) != 0 {
		t.Fatalf("missing suggestions = %#v", got)
	}
}

func TestRSSDateFallsBackToRawValue(t *testing.T) {
	if got := rssDate("not-a-date"); got != "not-a-date" {
		t.Fatalf("rssDate = %q", got)
	}
}
