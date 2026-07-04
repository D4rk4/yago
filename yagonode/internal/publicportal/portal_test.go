package publicportal

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeSource struct {
	results SearchResults
	err     error
}

func (f fakeSource) Search(context.Context, string) (SearchResults, error) {
	return f.results, f.err
}

func get(t *testing.T, portal *Portal, target string) (int, string) {
	t.Helper()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	portal.ServeHTTP(rec, req)

	resp := rec.Result()
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	return resp.StatusCode, string(body)
}

func TestPortalHomepageWithoutQuery(t *testing.T) {
	t.Parallel()

	status, body := get(t, New(fakeSource{}), "/")
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	for _, want := range []string{
		"yago",
		`name="q"`,
		"home",
		`rel="search" type="application/opensearchdescription+xml"`,
		`href="/opensearch.xml"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("homepage missing %q", want)
		}
	}
	if strings.Contains(body, "result(s) for") {
		t.Fatal("no results before a query")
	}
}

func TestPortalRendersResultsWithMarker(t *testing.T) {
	t.Parallel()

	source := fakeSource{results: SearchResults{
		Query:        "go",
		TotalResults: 2,
		Results: []SearchResult{
			{
				Title:      "Local hit",
				URL:        "http://a.example/1",
				DisplayURL: "a.example/1",
				Snippet:    "s",
			},
			{Title: "Web hit", URL: "http://b.example/2", DisplayURL: "b.example/2", Marked: true},
		},
	}}
	status, body := get(t, New(source), "/?q=go")
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	for _, want := range []string{"Local hit", "Web hit", "[ddgs]", "result(s) for"} {
		if !strings.Contains(body, want) {
			t.Fatalf("results missing %q", want)
		}
	}
}

func TestPortalEmptyResults(t *testing.T) {
	t.Parallel()

	status, body := get(t, New(fakeSource{results: SearchResults{Query: "x"}}), "/?q=x")
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	if !strings.Contains(body, "Nothing found.") {
		t.Fatal("expected empty results message")
	}
}

func TestPortalSearchErrorIsGeneric(t *testing.T) {
	t.Parallel()

	status, body := get(t, New(fakeSource{err: errors.New("backend detail")}), "/?q=go")
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	if !strings.Contains(body, "temporarily unavailable") {
		t.Fatal("expected generic error message")
	}
	if strings.Contains(body, "backend detail") {
		t.Fatal("must not leak internal error detail")
	}
}

func TestPortalRejectsNonGet(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	New(fakeSource{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", rec.Code)
	}
}
