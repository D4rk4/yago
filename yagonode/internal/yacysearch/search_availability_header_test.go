package yacysearch

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func availabilityRequest(t *testing.T) *http.Request {
	t.Helper()

	return httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/yacysearch.json?query=montelibero",
		nil,
	)
}

// The captured production answer: every source failed, so the node returned
// zero rows it cannot vouch for. The body has to stay exactly what a YaCy
// client already parses, and the header is the only thing that says the zero is
// not a statement about the index.
func TestUnprovenZeroIsLabelledWithoutChangingTheBody(t *testing.T) {
	search := &fakeSearch{response: searchcore.Response{
		PartialFailures: []searchcore.PartialFailure{
			{Source: "fuzzy-stage", Reason: "fuzzy search deadline exceeded"},
			{Source: "ddgs", Reason: "web-search fallback provider failed"},
		},
	}}
	rec := httptest.NewRecorder()
	jsonEndpoint{search: search, suggestions: newRecentQueries()}.
		ServeHTTP(rec, availabilityRequest(t))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for YaCy wire parity", rec.Code)
	}
	if got := rec.Header().Get(searchAvailabilityHeader); got != searchAvailabilityIncomplete {
		t.Fatalf("%s = %q, want %q", searchAvailabilityHeader, got, searchAvailabilityIncomplete)
	}
	if got := rec.Header().Get("Retry-After"); got != searchAvailabilityRetryAfter {
		t.Fatalf("Retry-After = %q, want %q", got, searchAvailabilityRetryAfter)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"totalResults":"0"`) {
		t.Fatalf("body no longer reports a zero total: %s", body)
	}
}

// A search whose sources all answered and found nothing is a truthful zero. It
// must carry no availability header at all, or every empty query would read as
// a node malfunction.
func TestTruthfulZeroCarriesNoAvailabilityHeader(t *testing.T) {
	search := &fakeSearch{response: searchcore.Response{
		Availability: searchcore.ResultAvailability{Exhausted: true},
	}}
	rec := httptest.NewRecorder()
	jsonEndpoint{search: search, suggestions: newRecentQueries()}.
		ServeHTTP(rec, availabilityRequest(t))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get(searchAvailabilityHeader); got != "" {
		t.Fatalf("%s = %q on a truthful zero", searchAvailabilityHeader, got)
	}
	if got := rec.Header().Get("Retry-After"); got != "" {
		t.Fatalf("Retry-After = %q on a truthful zero", got)
	}
}

// Results plus one lost source is a partial answer, not an unproven zero: the
// caller has rows to show and must not be told to retry.
func TestAnsweredSearchWithOneLostSourceIsNotLabelled(t *testing.T) {
	search := &fakeSearch{response: searchcore.Response{
		TotalResults:    1,
		Results:         []searchcore.Result{{Title: "Result", URL: "https://example.org/doc"}},
		PartialFailures: []searchcore.PartialFailure{{Source: "peer", Reason: "transport failed"}},
	}}
	rec := httptest.NewRecorder()
	jsonEndpoint{search: search, suggestions: newRecentQueries()}.
		ServeHTTP(rec, availabilityRequest(t))

	if got := rec.Header().Get(searchAvailabilityHeader); got != "" {
		t.Fatalf("%s = %q on an answered search", searchAvailabilityHeader, got)
	}
}

// RSS was the only surface that reported a lost source and an empty index with
// identical bytes.
func TestRSSReportsLostSourcesAndLabelsTheZero(t *testing.T) {
	search := &fakeSearch{response: searchcore.Response{
		PartialFailures: []searchcore.PartialFailure{
			{Source: "fuzzy-stage", Reason: "fuzzy search deadline exceeded"},
		},
	}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/yacysearch.rss?query=montelibero",
		nil,
	)
	rssEndpoint{search: search, suggestions: newRecentQueries()}.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get(searchAvailabilityHeader); got != searchAvailabilityIncomplete {
		t.Fatalf("%s = %q", searchAvailabilityHeader, got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<yacy:partialFailure source="fuzzy-stage"`) {
		t.Fatalf("rss body does not name the lost source: %s", body)
	}
	var feed struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal(rec.Body.Bytes(), &feed); err != nil {
		t.Fatalf("rss body is not well-formed: %v", err)
	}
}

// A complete RSS answer keeps the exact element set YaCy clients parse.
func TestRSSOmitsThePartialFailureElementWhenNothingFailed(t *testing.T) {
	search := &fakeSearch{response: searchcore.Response{
		Availability: searchcore.ResultAvailability{Exhausted: true},
	}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/yacysearch.rss?query=montelibero",
		nil,
	)
	rssEndpoint{search: search, suggestions: newRecentQueries()}.ServeHTTP(rec, req)

	if body := rec.Body.String(); strings.Contains(body, "partialFailure") {
		t.Fatalf("complete answer grew a partialFailure element: %s", body)
	}
	if got := rec.Header().Get(searchAvailabilityHeader); got != "" {
		t.Fatalf("%s = %q on a complete answer", searchAvailabilityHeader, got)
	}
}

// The browser surface carries the same label, so an operator reloading the page
// sees a retry hint rather than an assertion that the index is empty.
func TestHTMLSurfaceLabelsTheUnprovenZero(t *testing.T) {
	search := &fakeSearch{response: searchcore.Response{
		PartialFailures: []searchcore.PartialFailure{{Source: "local-search", Reason: "shed"}},
	}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/yacysearch.html?query=montelibero",
		nil,
	)
	htmlEndpoint{search: search, suggestions: newRecentQueries()}.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get(searchAvailabilityHeader); got != searchAvailabilityIncomplete {
		t.Fatalf("%s = %q", searchAvailabilityHeader, got)
	}
}

func TestResponseRSSPartialFailuresIsEmptyWhenNothingFailed(t *testing.T) {
	if rows := responseRSSPartialFailures(nil); rows != nil {
		t.Fatalf("rows = %v, want nil so the element is omitted", rows)
	}
}

// An RSS answer that returned rows names the source it lost but must not be
// labelled unproven: the caller has results to render.
func TestRSSNamesALostSourceWithoutLabellingAnAnsweredSearch(t *testing.T) {
	search := &fakeSearch{response: searchcore.Response{
		TotalResults:    1,
		Results:         []searchcore.Result{{Title: "Result", URL: "https://example.org/doc"}},
		PartialFailures: []searchcore.PartialFailure{{Source: "peer", Reason: "status 500"}},
	}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/yacysearch.rss?query=montelibero",
		nil,
	)
	rssEndpoint{search: search, suggestions: newRecentQueries()}.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), `<yacy:partialFailure source="peer"`) {
		t.Fatalf("rss body does not name the lost source: %s", rec.Body.String())
	}
	if got := rec.Header().Get(searchAvailabilityHeader); got != "" {
		t.Fatalf("%s = %q on an answered search", searchAvailabilityHeader, got)
	}
}
