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
	results   SearchResults
	err       error
	calls     int
	gotOffset int
	gotLimit  int
}

func (f *fakeSource) Search(
	_ context.Context,
	_ string,
	_ string,
	offset, limit int,
) (SearchResults, error) {
	f.calls++
	f.gotOffset = offset
	f.gotLimit = limit

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

	status, body := get(t, New(&fakeSource{}, false), "/")
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
	if strings.Contains(body, "available in this search window") {
		t.Fatal("no results before a query")
	}
}

func TestPortalRendersResultsWithProvenance(t *testing.T) {
	t.Parallel()

	source := &fakeSource{results: SearchResults{
		Query:        "go",
		TotalResults: 2,
		Results: []SearchResult{
			{
				Title:      "Local hit",
				URL:        "http://a.example/1",
				DisplayURL: "a.example/1",
				Snippet:    "s",
			},
			{
				Title:      "Web hit",
				URL:        "http://b.example/2",
				DisplayURL: "b.example/2",
				Provenance: "web",
			},
		},
	}}
	status, body := get(t, New(source, false), "/?q=go")
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	for _, want := range []string{
		"Local hit", "Web hit", `class="prov prov-web">web</span>`,
		"Up to 2 result(s) available in this search window",
		`rel="noreferrer nofollow"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("results missing %q", want)
		}
	}
}

func TestPortalEmptyResults(t *testing.T) {
	t.Parallel()

	status, body := get(t, New(&fakeSource{results: SearchResults{Query: "x"}}, false), "/?q=x")
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	if !strings.Contains(body, "Nothing found.") {
		t.Fatal("expected empty results message")
	}
}

func TestPortalDistinguishesIncompleteEmptyResponse(t *testing.T) {
	t.Parallel()

	status, body := get(t, New(&fakeSource{results: SearchResults{
		Query: "x", Incomplete: true, FederationUnavailable: true, PeersFailed: 2,
	}}, false), "/?q=x")
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	for _, want := range []string{
		"Some enabled search sources were unavailable",
		"no complete result set is available",
		"Peer federation was unavailable for part of this search",
		"2 identified peer response(s) failed",
		"No results are currently available",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("incomplete response missing %q", want)
		}
	}
	if strings.Contains(body, "0 result(s) for") ||
		strings.Contains(body, "No results matched") {
		t.Fatal("incomplete response must not claim an honest zero-result search")
	}
}

func TestPortalSearchErrorIsGeneric(t *testing.T) {
	t.Parallel()

	status, body := get(t, New(&fakeSource{err: errors.New("backend detail")}, false), "/?q=go")
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
	New(&fakeSource{}, false).ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestPortalPaginationParsesPageIntoOffset(t *testing.T) {
	t.Parallel()

	source := &fakeSource{results: SearchResults{
		Query:        "go",
		TotalResults: 100,
		Availability: SearchAvailability{Materialized: 21},
		Results:      []SearchResult{{Title: "hit", URL: "http://a.example/1"}},
	}}
	if _, body := get(
		t,
		New(source, false),
		"/?q=go&p=3",
	); !strings.Contains(
		body,
		`<span class="page" aria-current="page">3</span>`,
	) {
		t.Fatalf("body missing the current page indicator")
	}
	if source.gotOffset != 2*portalPageSize || source.gotLimit != portalPageSize {
		t.Fatalf("offset=%d limit=%d, want offset=%d limit=%d",
			source.gotOffset, source.gotLimit, 2*portalPageSize, portalPageSize)
	}
}

func TestPortalPaginationRendersPrevAndNext(t *testing.T) {
	t.Parallel()

	source := &fakeSource{results: SearchResults{
		Query:        "go",
		TotalResults: 100,
		Availability: SearchAvailability{Materialized: 21},
		Results:      []SearchResult{{Title: "hit", URL: "http://a.example/1"}},
	}}
	_, body := get(t, New(source, false), "/?q=go&p=2")

	for _, want := range []string{
		"‹ Previous", "Next ›", `<span class="page" aria-current="page">2</span>`, `rel="prev"`, `rel="next"`,
		"p=1", "p=3", "q=go",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("paginated results missing %q", want)
		}
	}
}

func TestPortalPaginationHidesNavOnSinglePage(t *testing.T) {
	t.Parallel()

	source := &fakeSource{results: SearchResults{
		Query:        "go",
		TotalResults: 2,
		Results:      []SearchResult{{Title: "a", URL: "http://a"}, {Title: "b", URL: "http://b"}},
	}}
	_, body := get(t, New(source, false), "/?q=go")

	if strings.Contains(body, "Next ›") || strings.Contains(body, "‹ Previous") {
		t.Fatal("single page should render no pager navigation")
	}
}

func TestPortalPaginationClampsPage(t *testing.T) {
	t.Parallel()

	junk := &fakeSource{results: SearchResults{Query: "go", TotalResults: 5}}
	get(t, New(junk, false), "/?q=go&p=not-a-number")
	if junk.gotOffset != 0 {
		t.Fatalf("junk page offset = %d, want 0 (page 1)", junk.gotOffset)
	}

	over := &fakeSource{results: SearchResults{
		Query: "go", TotalResults: 5,
		Availability: SearchAvailability{Materialized: 5, Exhausted: true},
	}}
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/?q=go&dom=image&p=99999",
		nil,
	)
	recorder := httptest.NewRecorder()
	New(over, false).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("over-max status = %d, want %d", recorder.Code, http.StatusSeeOther)
	}
	if location := recorder.Header().Get("Location"); location != "/?dom=image&p=1&q=go" {
		t.Fatalf("over-max location = %q, want %q", location, "/?dom=image&p=1&q=go")
	}
	if over.gotOffset != (portalMaxPage-1)*portalPageSize {
		t.Fatalf("over-max page offset = %d, want %d",
			over.gotOffset, (portalMaxPage-1)*portalPageSize)
	}
	if over.calls != 1 {
		t.Fatalf("over-max search calls = %d, want 1", over.calls)
	}

	computedLast := &fakeSource{results: SearchResults{
		Query: "go", TotalResults: 45,
		Availability: SearchAvailability{Materialized: 45, Exhausted: true},
	}}
	req = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?q=go&p=99999", nil)
	recorder = httptest.NewRecorder()
	New(computedLast, false).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusSeeOther ||
		recorder.Header().Get("Location") != "/?p=5&q=go" {
		t.Fatalf(
			"computed-last redirect = %d %q, want %d %q",
			recorder.Code,
			recorder.Header().Get("Location"),
			http.StatusSeeOther,
			"/?p=5&q=go",
		)
	}
}
