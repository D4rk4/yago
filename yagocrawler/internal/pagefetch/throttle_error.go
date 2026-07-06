package pagefetch

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// ThrottledError is the page rejection for a server-signalled overload — 429
// Too Many Requests or 503 Service Unavailable — optionally carrying the
// server's Retry-After wish. It wraps ErrPageRejected so existing rejection
// handling keeps working, while politeness code can read the throttle signal
// and back the host off (RFC 9110 §10.2.3; RFC 6585 §4).
type ThrottledError struct {
	Status     int
	RetryAfter time.Duration
}

func (e *ThrottledError) Error() string {
	return fmt.Sprintf("status %d: %v", e.Status, ErrPageRejected)
}

func (e *ThrottledError) Unwrap() error { return ErrPageRejected }

// AsThrottled unwraps a fetch error to its throttle signal, if it carries one.
func AsThrottled(err error) (*ThrottledError, bool) {
	var throttled *ThrottledError
	if errors.As(err, &throttled) {
		return throttled, true
	}

	return nil, false
}

// ThrottledStatus reports whether an HTTP status is a server-load signal the
// crawler must back off from rather than retry through heavier means.
func ThrottledStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable
}

// ParseRetryAfter reads a Retry-After header value — RFC 9110 allows both
// delay-seconds and an HTTP-date — returning zero for an absent or unreadable
// value; negative results (a date in the past) also fold to zero.
func ParseRetryAfter(value string, now time.Time) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}

		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		if wait := at.Sub(now); wait > 0 {
			return wait
		}
	}

	return 0
}
