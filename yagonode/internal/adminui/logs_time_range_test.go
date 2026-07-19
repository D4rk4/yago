package adminui

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFilterLogEntriesUsesTimestampOrdering(t *testing.T) {
	t.Parallel()

	bounds, message := validatedLogTimeRange("2026-07-04T00:00:00", "2026-07-04T01:00:00")
	if message != "" {
		t.Fatalf("validation = %q", message)
	}
	entries := []LogEntry{
		{Time: "2026-07-04T01:30:00+02:00", Name: "earlier"},
		{Time: "2026-07-04T00:30:00Z", Name: "inside"},
		{Time: "2026-07-04T01:30:00Z", Name: "later"},
		{Time: "not-a-time", Name: "malformed"},
	}
	got := filterLogEntriesInTimeRange(entries, bounds)
	if len(got) != 1 || got[0].Name != "inside" {
		t.Fatalf("filtered = %+v", got)
	}
}

func TestValidatedLogTimeRangeRejectsMalformedAndReversedBounds(t *testing.T) {
	t.Parallel()

	if _, message := validatedLogTimeRange("bad", ""); message != invalidLogFromMessage {
		t.Fatalf("from validation = %q", message)
	}
	if _, message := validatedLogTimeRange("", "bad"); message != invalidLogToMessage {
		t.Fatalf("to validation = %q", message)
	}
	if _, message := validatedLogTimeRange(
		"2026-07-04T01:00",
		"2026-07-04T00:00",
	); message != invalidLogOrderMessage {
		t.Fatalf("order validation = %q", message)
	}
}

func TestParseLogTimeInputNormalizesRFC3339Offset(t *testing.T) {
	t.Parallel()

	parsed, valid := parseLogTimeInput("2026-07-04T02:30:00.123+02:00")
	want := time.Date(2026, time.July, 4, 0, 30, 0, 123000000, time.UTC)
	if !valid || !parsed.Equal(want) || parsed.Location() != time.UTC {
		t.Fatalf("parsed RFC3339 input = %v/%t, want %v", parsed, valid, want)
	}
}

func TestConsoleLogsPreservesUTCTimeRangeAcrossRefresh(t *testing.T) {
	t.Parallel()

	entries := []LogEntry{
		{Time: "2026-07-03T23:59:59Z", Severity: "info", Name: "before"},
		{Time: "2026-07-04T00:30:00Z", Severity: "info", Name: "inside"},
		{Time: "2026-07-04T01:00:01Z", Severity: "info", Name: "after"},
	}
	page := do(
		t,
		New(Options{Logs: fakeLogs{entries: entries}}),
		"/admin/logs?from=2026-07-04T00:00:00&to=2026-07-04T01:00:00",
	)
	if page.status != http.StatusOK {
		t.Fatalf("status = %d", page.status)
	}
	if !strings.Contains(page.body, "inside") ||
		strings.Contains(page.body, ">before<") || strings.Contains(page.body, ">after<") {
		t.Fatalf("time-filtered page = %s", page.body)
	}
	for _, fragment := range []string{
		`name="from" value="2026-07-04T00:00:00"`,
		`name="to" value="2026-07-04T01:00:00"`,
		"from=2026-07-04T00%3A00%3A00",
		"to=2026-07-04T01%3A00%3A00",
		"— filtered",
	} {
		if !strings.Contains(page.body, fragment) {
			t.Fatalf("time-filtered page missing %q", fragment)
		}
	}
}

func TestConsoleLogsComposesTimeRangeWithExistingFilters(t *testing.T) {
	t.Parallel()

	entries := []LogEntry{
		{
			Time:     "2026-07-04T00:30:00Z",
			Severity: "info",
			Category: "crawl",
			Name:     "match",
			Message:  "needle",
		},
		{
			Time:     "2026-07-04T00:30:00Z",
			Severity: "warn",
			Category: "crawl",
			Name:     "wrong-severity",
			Message:  "needle",
		},
		{
			Time:     "2026-07-04T00:30:00Z",
			Severity: "info",
			Category: "search",
			Name:     "wrong-category",
			Message:  "needle",
		},
		{
			Time:     "2026-07-04T00:30:00Z",
			Severity: "info",
			Category: "crawl",
			Name:     "wrong-text",
			Message:  "other",
		},
		{
			Time:     "2026-07-04T02:00:00Z",
			Severity: "info",
			Category: "crawl",
			Name:     "wrong-time",
			Message:  "needle",
		},
	}
	page := do(
		t,
		New(Options{Logs: fakeLogs{entries: entries}}),
		"/admin/logs?severity=info&category=crawl&q=needle&from=2026-07-04T00:00&to=2026-07-04T01:00",
	)
	if !strings.Contains(page.body, ">match<") {
		t.Fatalf("matching event missing: %s", page.body)
	}
	for _, name := range []string{"wrong-severity", "wrong-category", "wrong-text", "wrong-time"} {
		if strings.Contains(page.body, ">"+name+"<") {
			t.Fatalf("event %q escaped composed filters", name)
		}
	}
	for _, fragment := range []string{
		"severity=info",
		"category=crawl",
		"q=needle",
		"from=2026-07-04T00%3A00",
		"to=2026-07-04T01%3A00",
	} {
		if !strings.Contains(page.body, fragment) {
			t.Fatalf("refresh URL missing %q", fragment)
		}
	}
}

func TestConsoleLogsPartialReportsAndPreservesInvalidTimeRange(t *testing.T) {
	t.Parallel()

	page := do(
		t,
		New(Options{Logs: fakeLogs{entries: []LogEntry{{
			Time: "2026-07-04T00:30:00Z", Name: "hidden",
		}}}}),
		"/admin/logs/events?from=invalid&to=2026-07-04T01:00:00",
	)
	if page.status != http.StatusOK || !strings.Contains(page.body, invalidLogFromMessage) ||
		!strings.Contains(page.body, "from=invalid") ||
		!strings.Contains(page.body, "to=2026-07-04T01%3A00%3A00") ||
		strings.Contains(page.body, ">hidden<") {
		t.Fatalf("invalid range partial = %d %s", page.status, page.body)
	}
}
