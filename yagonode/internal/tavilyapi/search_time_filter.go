package tavilyapi

import (
	"strings"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
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

// requestFirstSeenBounds resolves the first-seen window, which asks when this
// node first saw a document rather than when the document was published. It is
// a yago extension, so it is stated explicitly and only explicitly: time_range,
// days, and topic keep their upstream publication-recency meaning and never
// produce a first-seen bound. Both values were validated earlier.
func requestFirstSeenBounds(req SearchRequest) (time.Time, time.Time) {
	start, _ := parseOptionalDate(req.FirstSeenStart, "first_seen_start")
	end, _ := parseOptionalDate(req.FirstSeenEnd, "first_seen_end")
	if !end.IsZero() {
		// Include the whole end day.
		end = end.Add(24*time.Hour - time.Nanosecond)
	}

	return start, end
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

// resultsWithinRequestedBounds holds the served rows to the caller's own time
// windows. Remote and web rows bypassed the local index filter, so the
// document-date bounds are re-applied to them here.
//
// A first-seen bound is different in kind. First-seen is when this node saw the
// document, peers neither send nor receive it, so a row this node does not hold
// carries no first-seen time at all and cannot be shown to fall inside the
// window. Such a row therefore drops under an active first-seen bound, which is
// the same refusal the index applies to a document with no first-seen time.
// Retrieval itself is unchanged: local and peer stages still run, and a caller
// whose own window keeps nothing gets an honest empty answer, not a failure.
func resultsWithinRequestedBounds(
	results []searchcore.Result,
	req searchcore.Request,
) []searchcore.Result {
	firstSeenBounded := req.FirstSeenBounded()
	if req.MinDate.IsZero() && req.MaxDate.IsZero() && !firstSeenBounded {
		return results
	}
	bounded := make([]searchcore.Result, 0, len(results))
	for _, result := range results {
		if !resultWithinBounds(result.Date, req.MinDate, req.MaxDate) {
			continue
		}
		if firstSeenBounded && !result.StoredLocally() {
			continue
		}
		bounded = append(bounded, result)
	}

	return bounded
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
