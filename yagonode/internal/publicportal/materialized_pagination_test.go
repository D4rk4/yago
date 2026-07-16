package publicportal

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPortalEstimatedTotalDoesNotAdvertiseUnmaterializedPage(t *testing.T) {
	t.Parallel()

	results := make([]SearchResult, 8)
	for index := range results {
		results[index] = SearchResult{
			Title: fmt.Sprintf("result-%d", index),
			URL:   fmt.Sprintf("https://example.test/%d", index),
		}
	}
	source := &fakeSource{results: SearchResults{
		Query: "go", TotalResults: 119,
		Availability: SearchAvailability{Materialized: len(results)},
		Incomplete:   true,
		Results:      results,
	}}
	status, body := get(t, New(source, false), "/?q=go")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if strings.Contains(body, "Next ›") || strings.Contains(body, "p=2") {
		t.Fatal("estimated total advertised an unmaterialized second page")
	}
}

func TestPortalUnconfirmedDeepPageLinksOnlyConfirmedPrefix(t *testing.T) {
	t.Parallel()

	source := &fakeSource{results: SearchResults{
		Query: "go", TotalResults: 119,
		Availability: SearchAvailability{Materialized: 7},
		Incomplete:   true,
	}}
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/?q=go&dom=image&p=50",
		nil,
	)
	recorder := httptest.NewRecorder()
	New(source, false).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Location") != "" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Header().Get("Location"))
	}
	body := recorder.Body.String()
	for _, want := range []string{
		`rel="prev" href="/?dom=image&amp;p=1&amp;q=go"`,
		`<span class="page" aria-current="page">50</span>`,
		"Some enabled search sources were unavailable",
		"This page is beyond the confirmed result window",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q", want)
		}
	}
	for page := 2; page < 50; page++ {
		if strings.Contains(body, fmt.Sprintf("p=%d", page)) {
			t.Fatalf("body links unmaterialized page %d", page)
		}
	}
}

func TestPortalUnconfirmedDeepPageWithoutMaterializedRowsReturnsToFirstPage(t *testing.T) {
	t.Parallel()

	source := &fakeSource{results: SearchResults{
		Query: "go", TotalResults: 119, Incomplete: true,
	}}
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/?q=go&p=50",
		nil,
	)
	recorder := httptest.NewRecorder()
	New(source, false).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Location") != "" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Header().Get("Location"))
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `rel="prev" href="/?p=1&amp;q=go"`) {
		t.Fatalf("body has no direct first-page link: %s", body)
	}
	for page := 2; page < 50; page++ {
		if strings.Contains(body, fmt.Sprintf("p=%d", page)) {
			t.Fatalf("body links unmaterialized page %d", page)
		}
	}
}

func TestPortalExhaustedPageRedirectsToMaterializedTail(t *testing.T) {
	t.Parallel()

	source := &fakeSource{results: SearchResults{
		Query: "go", TotalResults: 21,
		Availability: SearchAvailability{Materialized: 21, Exhausted: true},
	}}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?q=go&p=4", nil)
	recorder := httptest.NewRecorder()
	New(source, false).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/?p=3&q=go" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Header().Get("Location"))
	}
}
