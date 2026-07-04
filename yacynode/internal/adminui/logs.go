package adminui

import "context"

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
