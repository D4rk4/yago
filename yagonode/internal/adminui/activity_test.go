package adminui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeActivity struct{ view ActivityView }

func (f fakeActivity) Activity(context.Context) ActivityView { return f.view }

// TestActivityPageRendersPerMode pins UI-16's page: full mode shows the query
// column and top words, aggregate mode never leaks a query cell, and a nil
// source renders the unavailable notice.
func TestActivityPageRendersPerMode(t *testing.T) {
	full := New(Options{Activity: fakeActivity{view: ActivityView{
		Mode: "full", Total: 12, ZeroResults: 3,
		Entries: []ActivityEntry{{
			Time: "10:31:00", Query: "осень ддт", Length: 9, Terms: 2,
			Results: 4, Duration: "1.5s", Source: "global",
		}},
		TopWords: []ActivityWord{{Word: "осень", Count: 2}},
	}}})
	page := consoleGet(t, full, "/admin/activity")
	for _, want := range []string{"Search activity", "осень ддт", "Top query words", ">12<"} {
		if !strings.Contains(page, want) {
			t.Fatalf("full page misses %q", want)
		}
	}

	aggregate := New(Options{Activity: fakeActivity{view: ActivityView{
		Mode: "aggregate", Total: 2,
		Entries: []ActivityEntry{{
			Time: "10:31:00", Length: 9, Terms: 2, Results: 4,
			Duration: "1.5s", Source: "local",
		}},
	}}})
	aggregatePage := consoleGet(t, aggregate, "/admin/activity")
	if strings.Contains(aggregatePage, "<th scope=\"col\">Query</th>") {
		t.Fatal("aggregate mode must not render a query column")
	}
	if !strings.Contains(aggregatePage, "aggregate") {
		t.Fatal("aggregate page must state its privacy mode")
	}

	unavailable := New(Options{})
	if page := consoleGet(t, unavailable, "/admin/activity"); !strings.Contains(
		page, "query logging is off",
	) {
		t.Fatalf("unavailable notice missing: %.120s", page)
	}
}

func consoleGet(t *testing.T, console http.Handler, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	console.ServeHTTP(rec, httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, path, nil,
	))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", path, rec.Code)
	}

	return rec.Body.String()
}
