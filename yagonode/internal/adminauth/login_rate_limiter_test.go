package adminauth

import (
	"testing"
	"time"
)

func TestLoginRateLimiterBlocksAfterMax(t *testing.T) {
	clock := &mutableClock{now: time.Unix(1000, 0)}
	limiter := newLoginRateLimiter(3, time.Minute, clock.Now)
	key := "10.0.0.1"
	for i := range 3 {
		if !limiter.allow(key) {
			t.Fatalf("attempt %d should be allowed", i)
		}
		limiter.recordFailure(key)
	}
	if limiter.allow(key) {
		t.Fatal("caller should be blocked after the maximum failures")
	}
}

func TestLoginRateLimiterExpiresWindow(t *testing.T) {
	clock := &mutableClock{now: time.Unix(1000, 0)}
	limiter := newLoginRateLimiter(2, time.Minute, clock.Now)
	key := "10.0.0.1"
	limiter.recordFailure(key)
	limiter.recordFailure(key)
	if limiter.allow(key) {
		t.Fatal("caller should be blocked after two failures")
	}

	clock.now = clock.now.Add(2 * time.Minute)
	if !limiter.allow(key) {
		t.Fatal("caller should be allowed once the window has passed")
	}
}

func TestLoginRateLimiterResetClears(t *testing.T) {
	clock := &mutableClock{now: time.Unix(1000, 0)}
	limiter := newLoginRateLimiter(2, time.Minute, clock.Now)
	key := "10.0.0.1"
	limiter.recordFailure(key)
	limiter.recordFailure(key)
	limiter.reset(key)
	if !limiter.allow(key) {
		t.Fatal("reset should clear recorded failures")
	}
}

func TestLoginRateLimiterIsolatesKeys(t *testing.T) {
	clock := &mutableClock{now: time.Unix(1000, 0)}
	limiter := newLoginRateLimiter(1, time.Minute, clock.Now)
	limiter.recordFailure("a")
	if limiter.allow("a") {
		t.Fatal("key a should be blocked")
	}
	if !limiter.allow("b") {
		t.Fatal("key b should be independent of key a")
	}
}
