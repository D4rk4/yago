package pagefetch

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestThrottledErrorWrapsPageRejection(t *testing.T) {
	err := fmt.Errorf("fetch: %w", &ThrottledError{Status: 429, RetryAfter: time.Minute})
	if !errors.Is(err, ErrPageRejected) {
		t.Fatal("throttle must count as a page rejection")
	}
	throttled, ok := AsThrottled(err)
	if !ok || throttled.Status != 429 || throttled.RetryAfter != time.Minute {
		t.Fatalf("AsThrottled = %#v, %v", throttled, ok)
	}
	if _, ok := AsThrottled(errors.New("plain")); ok {
		t.Fatal("plain error mistaken for throttle")
	}
	if err.Error() == "" {
		t.Fatal("empty error text")
	}
}

func TestThrottledStatusCoversLoadSignals(t *testing.T) {
	if !ThrottledStatus(429) || !ThrottledStatus(503) {
		t.Fatal("429/503 must be throttle signals")
	}
	if ThrottledStatus(404) || ThrottledStatus(500) {
		t.Fatal("non-load statuses must not throttle")
	}
}

func TestParseRetryAfterForms(t *testing.T) {
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	if got := ParseRetryAfter("120", now); got != 2*time.Minute {
		t.Fatalf("seconds form = %v", got)
	}
	future := now.Add(90 * time.Second).UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")
	if got := ParseRetryAfter(future, now); got != 90*time.Second {
		t.Fatalf("date form = %v", got)
	}
	for _, value := range []string{
		"", "junk", "-5", "0",
		now.Add(-time.Minute).UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT"),
	} {
		if got := ParseRetryAfter(value, now); got != 0 {
			t.Fatalf("ParseRetryAfter(%q) = %v, want 0", value, got)
		}
	}
}
