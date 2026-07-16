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
		Mode: "full", Total: 12, ConfirmedZeroResults: 3,
		Entries: []ActivityEntry{{
			Time: "2026-07-07 10:31:00 UTC", Query: "осень ддт", Length: 9, Terms: 2,
			Results: 4, ResultsKnown: true, Complete: true,
			Duration: "1.5s", Source: "global",
		}},
		TopWords: []ActivityWord{{Word: "осень", Count: 2}},
	}}})
	page := activityConsoleGet(t, full)
	for _, want := range []string{
		"Search activity",
		"осень ддт",
		"Top query words from up to 200 retained recent search attempts",
		"Occurrences",
		"Confirmed zero-result searches",
		"Search-window upper bound",
		"2026-07-07 10:31:00 UTC",
		"Up to 4",
		">12<",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("full page misses %q", want)
		}
	}

	aggregate := New(Options{Activity: fakeActivity{view: ActivityView{
		Mode: "aggregate", Total: 2,
		Entries: []ActivityEntry{
			{
				Time: "2026-07-07 10:31:00 UTC", Length: 9, Terms: 2, Results: 4,
				ResultsKnown: true, Duration: "1.5s", Source: "local",
			},
			{Time: "2026-07-07 10:32:00 UTC", Duration: "2s", Source: "global"},
		},
	}}})
	aggregatePage := activityConsoleGet(t, aggregate)
	if strings.Contains(aggregatePage, "<th scope=\"col\">Query</th>") {
		t.Fatal("aggregate mode must not render a query column")
	}
	if !strings.Contains(aggregatePage, "aggregate") {
		t.Fatal("aggregate page must state its privacy mode")
	}
	if !strings.Contains(aggregatePage, "Up to 4 (partial)") ||
		!strings.Contains(aggregatePage, "Unavailable") {
		t.Fatal("activity rows must distinguish partial and errored result windows")
	}
	emptyAggregate := New(Options{Activity: fakeActivity{view: ActivityView{Mode: "aggregate"}}})
	if page := activityConsoleGet(t, emptyAggregate); !strings.Contains(page, `colspan="6"`) {
		t.Fatal("aggregate empty state must span its six visible columns")
	}

	unavailable := New(Options{})
	if page := activityConsoleGet(t, unavailable); !strings.Contains(
		page, "query logging is off",
	) {
		t.Fatalf("unavailable notice missing: %.120s", page)
	}
}

func activityConsoleGet(t *testing.T, console http.Handler) string {
	return activityConsoleGetAt(t, console, "/admin/activity")
}

func activityConsoleGetAt(t *testing.T, console http.Handler, target string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	console.ServeHTTP(rec, httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, target, nil,
	))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", target, rec.Code)
	}

	return rec.Body.String()
}
