package adminui

import "testing"

// TestFilterLogEntriesByText pins UI-13: the free-text needle matches the
// message, the event name, and the category, case-insensitively, and composes
// with the severity filter.
func TestFilterLogEntriesByText(t *testing.T) {
	entries := []LogEntry{
		{
			Severity: "info",
			Category: "crawl",
			Name:     "crawl.started",
			Message:  "crawl of example.org began",
		},
		{Severity: "warn", Category: "search", Name: "peer.timeout", Message: "peer answered late"},
		{Severity: "info", Category: "config", Name: "node.started", Message: "node started"},
	}

	if got := filterLogEntries(entries, "", "", "EXAMPLE.ORG"); len(got) != 1 ||
		got[0].Name != "crawl.started" {
		t.Fatalf("message match = %+v", got)
	}
	if got := filterLogEntries(entries, "", "", "peer.timeout"); len(got) != 1 {
		t.Fatalf("name match = %+v", got)
	}
	if got := filterLogEntries(entries, "", "", "config"); len(got) != 1 {
		t.Fatalf("category match = %+v", got)
	}
	if got := filterLogEntries(entries, "warn", "", "peer"); len(got) != 1 {
		t.Fatalf("composed filter = %+v", got)
	}
	if got := filterLogEntries(entries, "info", "", "peer"); len(got) != 0 {
		t.Fatalf("severity must still gate: %+v", got)
	}
}
