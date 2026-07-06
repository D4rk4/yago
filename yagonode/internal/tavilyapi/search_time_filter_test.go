package tavilyapi

import (
	"testing"
	"time"
)

func withScriptedFilterClock(t *testing.T) time.Time {
	t.Helper()
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	old := timeFilterClock
	timeFilterClock = func() time.Time { return now }
	t.Cleanup(func() { timeFilterClock = old })

	return now
}

func TestRequestTimeBoundsPrecedence(t *testing.T) {
	now := withScriptedFilterClock(t)
	days := 5

	// Explicit dates win over everything.
	start, end := requestTimeBounds(SearchRequest{
		StartDate: "2026-01-01", EndDate: "2026-02-01",
		TimeRange: "week", Days: &days, Topic: "news",
	})
	if start != time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("start = %v", start)
	}
	if !end.After(time.Date(2026, 2, 1, 23, 59, 0, 0, time.UTC)) {
		t.Fatalf("end must cover the whole end day: %v", end)
	}

	// time_range beats days and topic.
	start, end = requestTimeBounds(SearchRequest{TimeRange: "d", Days: &days, Topic: "news"})
	if start != now.Add(-24*time.Hour) || !end.IsZero() {
		t.Fatalf("day range = %v %v", start, end)
	}
	for value, window := range map[string]time.Duration{
		"week": 7 * 24 * time.Hour, "m": 30 * 24 * time.Hour, "year": 365 * 24 * time.Hour,
	} {
		if start, _ = requestTimeBounds(
			SearchRequest{TimeRange: value},
		); start != now.Add(
			-window,
		) {
			t.Fatalf("%s range start = %v", value, start)
		}
	}

	// Legacy days beats the topic default.
	start, _ = requestTimeBounds(SearchRequest{Days: &days, Topic: "news"})
	if start != now.AddDate(0, 0, -5) {
		t.Fatalf("days start = %v", start)
	}

	// topic=news defaults to recent coverage; general stays unbounded.
	start, _ = requestTimeBounds(SearchRequest{Topic: "news"})
	if start != now.AddDate(0, 0, -newsDefaultDays) {
		t.Fatalf("news start = %v", start)
	}
	if start, end = requestTimeBounds(
		SearchRequest{Topic: "general"},
	); !start.IsZero() ||
		!end.IsZero() {
		t.Fatalf("general bounds = %v %v", start, end)
	}

	// A start date alone leaves the end open.
	if _, end = requestTimeBounds(SearchRequest{StartDate: "2026-01-01"}); !end.IsZero() {
		t.Fatalf("open end = %v", end)
	}
}

func TestResultWithinBounds(t *testing.T) {
	minDate := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	maxDate := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	if !resultWithinBounds("20260615", minDate, maxDate) {
		t.Fatal("compact in-range date must pass")
	}
	if !resultWithinBounds("2026-06-15", minDate, maxDate) {
		t.Fatal("dashed in-range date must pass")
	}
	if resultWithinBounds("20260501", minDate, maxDate) {
		t.Fatal("too-old date must drop")
	}
	if resultWithinBounds("20260715", minDate, maxDate) {
		t.Fatal("too-new date must drop")
	}
	if resultWithinBounds("", minDate, maxDate) {
		t.Fatal("undated result must drop under active bounds")
	}
	if !resultWithinBounds("", time.Time{}, time.Time{}) {
		t.Fatal("no bounds must pass everything")
	}
	if resultWithinBounds("junk", minDate, time.Time{}) {
		t.Fatal("unparsable date must drop")
	}
	if !resultWithinBounds("20260615", time.Time{}, maxDate) {
		t.Fatal("max-only bound must pass an early date")
	}
}

func TestSearchAppliesTimeBoundsToResults(t *testing.T) {
	withScriptedFilterClock(t)
	endpoint, search, _ := richSearchEndpoint()
	search.response.Results[0].Date = "2026-07-05"
	search.response.Results[1].Date = "2025-01-01"
	search.response.Results[1].URL = "https://old.example/doc"
	search.response.Results[1].Host = "old.example"

	resp, err := endpoint.searchResponse(
		t.Context(),
		SearchRequest{Query: "golang", TimeRange: "week"},
		timeFilterClock(),
		"id-1",
	)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].URL != "https://example.org/doc" {
		t.Fatalf("results = %+v, want the stale result dropped", resp.Results)
	}
	if !search.got.MinDate.Equal(timeFilterClock().Add(-7 * 24 * time.Hour)) {
		t.Fatalf("core min date = %v", search.got.MinDate)
	}

	if _, err := endpoint.searchResponse(
		t.Context(),
		SearchRequest{Query: "golang", Days: intPointer(-1)},
		timeFilterClock(),
		"id-2",
	); err == nil {
		t.Fatal("negative days must fail validation")
	}
}

func intPointer(v int) *int { return &v }

func TestExcludeDomainsWithoutDateBounds(t *testing.T) {
	endpoint, _, _ := richSearchEndpoint()
	resp, err := endpoint.searchResponse(
		t.Context(),
		SearchRequest{Query: "golang", ExcludeDomains: []string{"blocked.example"}},
		time.Now(),
		"id-3",
	)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	for _, result := range resp.Results {
		if result.URL == "https://blocked.example/doc" {
			t.Fatal("excluded domain served")
		}
	}
	if len(resp.Results) == 0 {
		t.Fatal("allowed results must remain")
	}
}
