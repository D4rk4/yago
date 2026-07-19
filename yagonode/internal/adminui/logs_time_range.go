package adminui

import (
	"strings"
	"time"
)

const (
	logTimeInputMinuteLayout = "2006-01-02T15:04"
	logTimeInputSecondLayout = "2006-01-02T15:04:05"
	invalidLogFromMessage    = "From must be a valid UTC date and time."
	invalidLogToMessage      = "To must be a valid UTC date and time."
	invalidLogOrderMessage   = "From must not be later than To."
)

type logTimeRange struct {
	from time.Time
	to   time.Time
}

func validatedLogTimeRange(fromValue, toValue string) (logTimeRange, string) {
	from, valid := parseLogTimeInput(fromValue)
	if strings.TrimSpace(fromValue) != "" && !valid {
		return logTimeRange{}, invalidLogFromMessage
	}
	to, valid := parseLogTimeInput(toValue)
	if strings.TrimSpace(toValue) != "" && !valid {
		return logTimeRange{}, invalidLogToMessage
	}
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		return logTimeRange{}, invalidLogOrderMessage
	}

	return logTimeRange{from: from, to: to}, ""
}

func parseLogTimeInput(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, true
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC(), true
	}
	for _, layout := range []string{logTimeInputSecondLayout, logTimeInputMinuteLayout} {
		if parsed, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			return parsed, true
		}
	}

	return time.Time{}, false
}

func filterLogEntriesInTimeRange(entries []LogEntry, bounds logTimeRange) []LogEntry {
	if bounds.from.IsZero() && bounds.to.IsZero() {
		return entries
	}
	filtered := make([]LogEntry, 0, len(entries))
	for _, entry := range entries {
		observed, err := time.Parse(time.RFC3339Nano, entry.Time)
		if err != nil {
			continue
		}
		if !bounds.from.IsZero() && observed.Before(bounds.from) {
			continue
		}
		if !bounds.to.IsZero() && observed.After(bounds.to) {
			continue
		}
		filtered = append(filtered, entry)
	}

	return filtered
}
