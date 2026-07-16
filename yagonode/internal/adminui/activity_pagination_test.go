package adminui

import (
	"fmt"
	"strings"
	"testing"
)

func activityEntryFixtures(total int) []ActivityEntry {
	entries := make([]ActivityEntry, total)
	for position := range entries {
		sequence := total - position
		entries[position] = ActivityEntry{
			Time:         fmt.Sprintf("2026-07-16 00:%02d:00 UTC", sequence),
			Query:        fmt.Sprintf("query-%03d", sequence),
			Length:       9,
			Terms:        1,
			Results:      sequence,
			ResultsKnown: true,
			Complete:     true,
			Duration:     "1ms",
			Source:       "local",
		}
	}

	return entries
}

func TestBuildActivityPaginationBoundsNewestFirstPages(t *testing.T) {
	t.Parallel()

	entries := activityEntryFixtures(45)
	first := buildActivityPagination(entries, "")
	if first.Total != 45 || first.Pages != 3 || first.Page != 1 ||
		len(first.Entries) != activityEntriesPerPage || first.Start != 1 || first.End != 20 ||
		first.HasPrev || !first.HasNext {
		t.Fatalf("first page = %+v", first)
	}
	if first.Entries[0].Query != "query-045" || first.Entries[19].Query != "query-026" {
		t.Fatalf(
			"first page bounds = %q through %q",
			first.Entries[0].Query,
			first.Entries[19].Query,
		)
	}
	if first.PrevURL != "" || first.NextURL != "/admin/activity?apage=2#recent-searches" {
		t.Fatalf("first navigation = %+v", first)
	}

	middle := buildActivityPagination(entries, "2")
	if middle.Page != 2 || len(middle.Entries) != activityEntriesPerPage ||
		middle.Start != 21 || middle.End != 40 || !middle.HasPrev || !middle.HasNext {
		t.Fatalf("middle page = %+v", middle)
	}
	if middle.Entries[0].Query != "query-025" || middle.Entries[19].Query != "query-006" {
		t.Fatalf(
			"middle page bounds = %q through %q",
			middle.Entries[0].Query,
			middle.Entries[19].Query,
		)
	}
	if middle.PrevURL != "/admin/activity#recent-searches" ||
		middle.NextURL != "/admin/activity?apage=3#recent-searches" {
		t.Fatalf("middle navigation = %+v", middle)
	}

	last := buildActivityPagination(entries, "3")
	if last.Page != 3 || len(last.Entries) != 5 || last.Start != 41 || last.End != 45 ||
		!last.HasPrev || last.HasNext {
		t.Fatalf("last page = %+v", last)
	}
	if last.Entries[0].Query != "query-005" || last.Entries[4].Query != "query-001" {
		t.Fatalf(
			"last page bounds = %q through %q",
			last.Entries[0].Query,
			last.Entries[4].Query,
		)
	}
}

func TestBuildActivityPaginationClampsInvalidAndEmptyPages(t *testing.T) {
	t.Parallel()

	entries := activityEntryFixtures(21)
	clamped := buildActivityPagination(entries, "99")
	if clamped.Page != 2 || len(clamped.Entries) != 1 ||
		clamped.PrevURL != "/admin/activity#recent-searches" || clamped.NextURL != "" {
		t.Fatalf("clamped page = %+v", clamped)
	}

	invalid := buildActivityPagination(entries, "not-a-page")
	if invalid.Page != 1 || invalid.PrevURL != "" ||
		invalid.NextURL != "/admin/activity?apage=2#recent-searches" {
		t.Fatalf("invalid page = %+v", invalid)
	}

	empty := buildActivityPagination(nil, "2")
	if empty.Page != 1 || empty.Pages != 1 || empty.Total != 0 ||
		len(empty.Entries) != 0 || empty.Start != 0 || empty.End != 0 ||
		empty.HasPrev || empty.HasNext || empty.PrevURL != "" || empty.NextURL != "" {
		t.Fatalf("empty page = %+v", empty)
	}
}

func TestActivityPagePaginatesEntriesWithoutNarrowingTotals(t *testing.T) {
	t.Parallel()

	entries := activityEntryFixtures(45)
	entries[0].Complete = false
	entries[1].ResultsKnown = false
	console := New(Options{Activity: fakeActivity{view: ActivityView{
		Mode:                 "full",
		Total:                1234,
		ConfirmedZeroResults: 317,
		Entries:              entries,
		TopWords:             []ActivityWord{{Word: "latest", Count: 99}},
	}}})

	first := activityConsoleGetAt(t, console, "/admin/activity")
	for _, expected := range []string{
		"query-045", "query-026", "Page 1 of 3", "recent searches 1–20 of 45",
		"Next ›", `aria-label="Search activity pages"`, ">1234<", ">317<",
		">latest<", ">99<", "Up to 45 (partial)", "Unavailable",
	} {
		if !strings.Contains(first, expected) {
			t.Fatalf("first page missing %q", expected)
		}
	}
	if strings.Contains(first, "query-025") || strings.Contains(first, "‹ Previous") {
		t.Fatal("first page rendered an older query or a previous link")
	}

	middle := activityConsoleGetAt(t, console, "/admin/activity?apage=2")
	for _, expected := range []string{
		"query-025", "query-006", "Page 2 of 3", "recent searches 21–40 of 45",
		"‹ Previous", "Next ›", ">1234<", ">317<", ">latest<", ">99<",
	} {
		if !strings.Contains(middle, expected) {
			t.Fatalf("middle page missing %q", expected)
		}
	}
	if strings.Contains(middle, "query-026") || strings.Contains(middle, "query-005") {
		t.Fatal("middle page rendered a query outside its twenty-row window")
	}

	last := activityConsoleGetAt(t, console, "/admin/activity?apage=3")
	for _, expected := range []string{
		"query-005", "query-001", "Page 3 of 3", "recent searches 41–45 of 45",
		"‹ Previous", ">1234<", ">317<", ">latest<", ">99<",
	} {
		if !strings.Contains(last, expected) {
			t.Fatalf("last page missing %q", expected)
		}
	}
	if strings.Contains(last, "query-006") || strings.Contains(last, "Next ›") {
		t.Fatal("last page rendered a newer query or a next link")
	}
}
