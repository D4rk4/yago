package yagocrawlcontract

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DefaultRecrawlInterval is the shipped recrawl cadence: an indexed page is
// re-fetched once it is older than thirty days, so the search index stays fresh
// instead of eternal. A crawl profile carrying this interval schedules its URLs
// in the recrawl frontier; a zero interval leaves them fetched once forever.
const DefaultRecrawlInterval = 30 * 24 * time.Hour

const (
	minRecrawlInterval = time.Hour
	maxRecrawlInterval = 365 * 24 * time.Hour
	recrawlDay         = 24 * time.Hour
	recrawlWeek        = 7 * recrawlDay
)

// errRecrawlRange is the single rejection message so every malformed or
// out-of-range recrawl interval reads the same to the operator.
var errRecrawlRange = fmt.Errorf("recrawl interval must be off, or between 1h and 365d")

// ParseRecrawlInterval reads an operator-friendly recrawl cadence. Empty, "0",
// or the sentinels "off"/"none"/"disabled" (any case) disable recrawling and
// return a zero duration. Otherwise the value is a Go duration ("720h", "90m")
// or an integer with a trailing "d" (days) or "w" (weeks) unit ("30d", "2w"),
// and it must fall between 1h and 365d; anything else is rejected.
func ParseRecrawlInterval(raw string) (time.Duration, error) {
	trimmed := strings.TrimSpace(raw)
	switch strings.ToLower(trimmed) {
	case "", "0", "off", "none", "disabled":
		return 0, nil
	}
	interval, err := parseRecrawlDuration(trimmed)
	if err != nil {
		return 0, err
	}
	if interval < minRecrawlInterval || interval > maxRecrawlInterval {
		return 0, errRecrawlRange
	}

	return interval, nil
}

// parseRecrawlDuration resolves a non-sentinel interval into a duration,
// honoring the calendar "d"/"w" units the stdlib duration parser lacks.
func parseRecrawlDuration(trimmed string) (time.Duration, error) {
	if unit, ok := recrawlCalendarUnit(trimmed); ok {
		return parseCalendarInterval(trimmed[:len(trimmed)-1], unit)
	}
	interval, err := time.ParseDuration(trimmed)
	if err != nil || interval < 0 {
		return 0, errRecrawlRange
	}

	return interval, nil
}

// recrawlCalendarUnit reports the day/week unit a raw value's trailing letter
// selects, if any.
func recrawlCalendarUnit(raw string) (time.Duration, bool) {
	if raw == "" {
		return 0, false
	}
	switch raw[len(raw)-1] {
	case 'd', 'D':
		return recrawlDay, true
	case 'w', 'W':
		return recrawlWeek, true
	default:
		return 0, false
	}
}

// parseCalendarInterval multiplies a non-negative integer count by a calendar
// unit, rejecting a non-integer count, a negative count, or an overflow.
func parseCalendarInterval(digits string, unit time.Duration) (time.Duration, error) {
	count, err := strconv.Atoi(strings.TrimSpace(digits))
	if err != nil || count < 0 {
		return 0, errRecrawlRange
	}
	interval := time.Duration(count) * unit
	if interval/unit != time.Duration(count) {
		return 0, errRecrawlRange
	}

	return interval, nil
}

// FormatRecrawlInterval renders a recrawl cadence the way ParseRecrawlInterval
// reads it back: zero is "off", a whole number of weeks is "Nw", a whole number
// of days is "Nd", and anything finer falls back to the Go duration string.
func FormatRecrawlInterval(d time.Duration) string {
	switch {
	case d <= 0:
		return "off"
	case d%recrawlWeek == 0:
		return strconv.FormatInt(int64(d/recrawlWeek), 10) + "w"
	case d%recrawlDay == 0:
		return strconv.FormatInt(int64(d/recrawlDay), 10) + "d"
	default:
		return d.String()
	}
}
