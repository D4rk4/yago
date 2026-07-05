package yacysearch

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestHTMLEndpointShowsQueryTime(t *testing.T) {
	base := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	calls := 0
	oldClock := htmlClock
	t.Cleanup(func() { htmlClock = oldClock })
	htmlClock = func() time.Time {
		calls++

		return base.Add(time.Duration(calls-1) * 130 * time.Millisecond)
	}

	search := &fakeSearch{response: searchcore.Response{
		TotalResults: 1,
		Results:      []searchcore.Result{{Title: "R", URL: "https://a.example/"}},
	}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "http://node.test/yacysearch.html?query=go", nil,
	)
	htmlEndpoint{search: search, suggestions: newRecentQueries()}.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "(0.13 s)") {
		t.Fatalf("query time missing: %s", rec.Body.String())
	}
}
