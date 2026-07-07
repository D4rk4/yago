package adminui

import (
	"context"
	"sort"
	"strings"
)

// LogEntry is one recorded node event rendered in the Logs section.
type LogEntry struct {
	Time     string
	Severity string
	Category string
	Name     string
	Message  string
}

// LogsSource supplies the recent-events list on each request, newest first.
type LogsSource interface {
	Logs(ctx context.Context) []LogEntry
}

// logSeverities is the fixed severity vocabulary offered in the Logs filter.
var logSeverities = []string{"debug", "info", "warn", "error"}

// logsView is the Logs section's render model: the filtered entries plus the
// active filter and the category vocabulary, so the filter form and the
// htmx-refresh URL both stay consistent with what the operator selected.
type logsView struct {
	Entries    []LogEntry
	Severity   string
	Category   string
	Query      string
	Severities []string
	Categories []string
}

// filterLogEntries keeps the entries matching the active severity and category
// (case-insensitive, exact) plus the free-text needle. An empty filter field
// matches everything.
func filterLogEntries(entries []LogEntry, severity, category, needle string) []LogEntry {
	if severity == "" && category == "" && needle == "" {
		return entries
	}

	needle = strings.ToLower(needle)
	filtered := make([]LogEntry, 0, len(entries))
	for _, entry := range entries {
		if severity != "" && !strings.EqualFold(entry.Severity, severity) {
			continue
		}
		if category != "" && !strings.EqualFold(entry.Category, category) {
			continue
		}
		if needle != "" && !logEntryMentions(entry, needle) {
			continue
		}
		filtered = append(filtered, entry)
	}

	return filtered
}

// logEntryMentions reports whether the folded needle appears in the entry's
// message, event name, or category — the server-side text filter (UI-13).
func logEntryMentions(entry LogEntry, needle string) bool {
	return strings.Contains(strings.ToLower(entry.Message), needle) ||
		strings.Contains(strings.ToLower(entry.Name), needle) ||
		strings.Contains(strings.ToLower(entry.Category), needle)
}

// distinctLogCategories returns the sorted set of categories present in the
// recent events, for the filter dropdown.
func distinctLogCategories(entries []LogEntry) []string {
	seen := make(map[string]struct{}, len(entries))
	var categories []string
	for _, entry := range entries {
		if entry.Category == "" {
			continue
		}
		if _, ok := seen[entry.Category]; ok {
			continue
		}
		seen[entry.Category] = struct{}{}
		categories = append(categories, entry.Category)
	}
	sort.Strings(categories)

	return categories
}
