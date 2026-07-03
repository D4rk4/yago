package adminauth

import (
	"testing"
	"time"
)

func TestAPIKeyRateLimiterCapsPerWindow(t *testing.T) {
	clock := &mutableClock{now: time.Unix(0, 0)}
	limiter := newAPIKeyRateLimiter(2, time.Minute, clock.Now)

	first := limiter.allow("k")
	second := limiter.allow("k")
	if !first || !second {
		t.Fatal("first two requests should be allowed")
	}
	if limiter.allow("k") {
		t.Fatal("third request should be blocked")
	}
}

func TestAPIKeyRateLimiterPrunesExpiredEvents(t *testing.T) {
	clock := &mutableClock{now: time.Unix(0, 0)}
	limiter := newAPIKeyRateLimiter(1, time.Minute, clock.Now)

	if !limiter.allow("k") {
		t.Fatal("first request should be allowed")
	}
	if limiter.allow("k") {
		t.Fatal("second request within the window should be blocked")
	}
	clock.now = clock.now.Add(2 * time.Minute)
	if !limiter.allow("k") {
		t.Fatal("request after the window should be allowed again")
	}
}

func TestAPIKeyRateLimiterSeparatesKeys(t *testing.T) {
	clock := &mutableClock{now: time.Unix(0, 0)}
	limiter := newAPIKeyRateLimiter(1, time.Minute, clock.Now)

	if !limiter.allow("a") || !limiter.allow("b") {
		t.Fatal("distinct keys should not share a bucket")
	}
}
