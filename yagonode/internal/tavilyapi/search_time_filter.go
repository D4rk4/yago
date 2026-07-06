package tavilyapi

import (
	"strings"
	"time"
)

// timeFilterClock feeds time_range resolution; tests substitute a scripted
// clock.
var timeFilterClock = time.Now

// newsDefaultDays mirrors Tavily: topic=news without explicit bounds returns
// recent coverage only.
const newsDefaultDays = 3

// requestTimeBounds resolves the request's recency controls into document-date
// bounds: explicit start/end dates win, then time_range, then the legacy days
// parameter, then the news-topic default. All values were validated earlier.
func requestTimeBounds(req SearchRequest) (time.Time, time.Time) {
	start, _ := parseOptionalDate(req.StartDate, "start_date")
	end, _ := parseOptionalDate(req.EndDate, "end_date")
	if !end.IsZero() {
		// Include the whole end day.
		end = end.Add(24*time.Hour - time.Nanosecond)
	}
	if !start.IsZero() || !end.IsZero() {
		return start, end
	}
	if window := timeRangeWindow(req.TimeRange); window > 0 {
		return timeFilterClock().Add(-window), time.Time{}
	}
	if req.Days != nil && *req.Days > 0 {
		return timeFilterClock().AddDate(0, 0, -*req.Days), time.Time{}
	}
	if strings.EqualFold(strings.TrimSpace(req.Topic), "news") {
		return timeFilterClock().AddDate(0, 0, -newsDefaultDays), time.Time{}
	}

	return time.Time{}, time.Time{}
}

// timeRangeWindow maps the documented time_range values onto durations.
func timeRangeWindow(value string) time.Duration {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "day", "d":
		return 24 * time.Hour
	case "week", "w":
		return 7 * 24 * time.Hour
	case "month", "m":
		return 30 * 24 * time.Hour
	case "year", "y":
		return 365 * 24 * time.Hour
	default:
		return 0
	}
}

// resultWithinBounds keeps remote and web results honest too: their document
// dates arrive as yyyymmdd strings; when a bound is active, undated results
// drop, matching the local index filter.
func resultWithinBounds(date string, minDate, maxDate time.Time) bool {
	if minDate.IsZero() && maxDate.IsZero() {
		return true
	}
	when, err := parseResultDate(strings.TrimSpace(date))
	if err != nil {
		return false
	}
	if !minDate.IsZero() && when.Before(minDate.Truncate(24*time.Hour)) {
		return false
	}
	if !maxDate.IsZero() && when.After(maxDate) {
		return false
	}

	return true
}

// parseResultDate accepts both document-date encodings in circulation: the
// compact yyyymmdd of local results and the dashed ISO date of peer results.
func parseResultDate(date string) (time.Time, error) {
	if when, err := time.Parse("20060102", date); err == nil {
		return when, nil
	}

	return time.Parse("2006-01-02", date) //nolint:wrapcheck // caller drops on error.
}
