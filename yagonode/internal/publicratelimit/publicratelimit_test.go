package publicratelimit

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func scriptedLimiter(tiers Tiers) (*Limiter, *time.Time) {
	limiter := NewLimiter(tiers)
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	limiter.now = func() time.Time { return now }

	return limiter, &now
}

func TestLimiterTiers(t *testing.T) {
	limiter, now := scriptedLimiter(Tiers{Per3Seconds: 2, PerMinute: 3, Per10Minutes: 4})

	for i := 0; i < 2; i++ {
		if ok, _ := limiter.Allow("1.2.3.4", false); !ok {
			t.Fatalf("request %d must fit the 3s tier", i)
		}
	}
	if ok, retry := limiter.Allow("1.2.3.4", false); ok || retry != 3*time.Second {
		t.Fatalf("3s tier breach = %v %v", ok, retry)
	}
	// Another client is unaffected.
	if ok, _ := limiter.Allow("5.6.7.8", false); !ok {
		t.Fatal("other clients must not share the budget")
	}

	// After the 3s window the minute tier binds.
	*now = now.Add(4 * time.Second)
	if ok, _ := limiter.Allow("1.2.3.4", false); !ok {
		t.Fatal("third request must fit the minute tier")
	}
	if ok, retry := limiter.Allow("1.2.3.4", false); ok || retry != time.Minute {
		t.Fatalf("minute tier breach = %v %v", ok, retry)
	}

	// After the minute the 10-minute tier binds.
	*now = now.Add(2 * time.Minute)
	if ok, _ := limiter.Allow("1.2.3.4", false); !ok {
		t.Fatal("fourth request must fit the 10-minute tier")
	}
	if ok, retry := limiter.Allow("1.2.3.4", false); ok || retry != 10*time.Minute {
		t.Fatalf("10-minute tier breach = %v %v", ok, retry)
	}

	// After the largest window everything resets.
	*now = now.Add(11 * time.Minute)
	if ok, _ := limiter.Allow("1.2.3.4", false); !ok {
		t.Fatal("stamps must expire with the largest window")
	}
}

func TestAuthenticatedMultiplierAndZeroBudget(t *testing.T) {
	limiter, _ := scriptedLimiter(Tiers{Per3Seconds: 1, PerMinute: 100, Per10Minutes: 100})
	if ok, _ := limiter.Allow("9.9.9.9", true); !ok {
		t.Fatal("first authenticated request must pass")
	}
	for i := 0; i < authenticatedMultiplier-1; i++ {
		if ok, _ := limiter.Allow("9.9.9.9", true); !ok {
			t.Fatalf("authenticated request %d must ride the multiplier", i)
		}
	}
	if ok, _ := limiter.Allow("9.9.9.9", true); ok {
		t.Fatal("authenticated budget must still bound")
	}

	zero, _ := scriptedLimiter(Tiers{})
	if ok, _ := zero.Allow("z", false); ok {
		t.Fatal("zero budget must deny")
	}
}

func TestLimiterEvictsStaleClients(t *testing.T) {
	limiter, now := scriptedLimiter(DefaultPublicTiers())
	for i := 0; i < maxTrackedClients; i++ {
		if ok, _ := limiter.Allow(fmt.Sprintf("10.0.%d.%d", i/256, i%256), false); !ok {
			t.Fatalf("fill request %d denied", i)
		}
	}
	// While every tracked client is still live, eviction keeps them all.
	*now = now.Add(time.Minute)
	if ok, _ := limiter.Allow("early-bird", false); !ok {
		t.Fatal("early client must pass while others are live")
	}
	if len(limiter.clients) <= maxTrackedClients {
		t.Fatalf("live clients evicted: %d", len(limiter.clients))
	}

	*now = now.Add(11 * time.Minute)
	if ok, _ := limiter.Allow("fresh-client", false); !ok {
		t.Fatal("fresh client must pass after eviction")
	}
	if len(limiter.clients) > 3 {
		t.Fatalf("stale clients kept: %d", len(limiter.clients))
	}
}

func TestWrapThrottlesSearchPathsOnly(t *testing.T) {
	limiter, _ := scriptedLimiter(Tiers{Per3Seconds: 1, PerMinute: 1, Per10Minutes: 1})
	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	})
	handler := Wrap(next, limiter, func(r *http.Request) bool {
		return r.Header.Get("Authorization") == "Bearer good"
	})

	do := func(target, remote, auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
		req.RemoteAddr = remote
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		return rec
	}

	if rec := do("/yacysearch.json?query=x", "203.0.113.9:1000", ""); rec.Code != http.StatusOK {
		t.Fatalf("first search = %d", rec.Code)
	}
	rec := do("/yacysearch.rss?query=x", "203.0.113.9:1001", "")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("second search = %d retry=%q", rec.Code, rec.Header().Get("Retry-After"))
	}
	// Unthrottled paths always pass.
	if rec := do("/favicon?host=x", "203.0.113.9:1002", ""); rec.Code != http.StatusOK {
		t.Fatalf("asset path = %d", rec.Code)
	}
	if rec := do("/", "203.0.113.9:1003", ""); rec.Code != http.StatusOK {
		t.Fatalf("bare portal = %d", rec.Code)
	}
	// Portal queries throttle.
	if rec := do("/?q=x", "203.0.113.9:1004", ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("portal query = %d", rec.Code)
	}
	// Suggest throttles.
	if rec := do(
		"/suggest.json?q=x",
		"203.0.113.9:1005",
		"",
	); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("suggest = %d", rec.Code)
	}
	if rec := do(
		"/searchclick",
		"203.0.113.9:1006",
		"",
	); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("search click = %d", rec.Code)
	}
	// Authenticated callers ride the multiplier.
	if rec := do(
		"/yacysearch.json?query=x",
		"203.0.113.9:1007",
		"Bearer good",
	); rec.Code != http.StatusOK {
		t.Fatalf("authenticated = %d", rec.Code)
	}
	// Loopback gets raised limits without credentials.
	if rec := do("/yacysearch.json?query=x", "127.0.0.1:9", ""); rec.Code != http.StatusOK {
		t.Fatalf("loopback = %d", rec.Code)
	}
	// A remote address without a port still keys.
	if rec := do("/yacysearch.json?query=x", "bad-remote", ""); rec.Code != http.StatusOK {
		t.Fatalf("portless remote = %d", rec.Code)
	}
	if calls == 0 {
		t.Fatal("next handler never ran")
	}
}

func TestWrapWithoutAuthCallback(t *testing.T) {
	limiter, _ := scriptedLimiter(Tiers{Per3Seconds: 1, PerMinute: 1, Per10Minutes: 1})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	plain := Wrap(next, limiter, nil)
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/yacysearch.json?query=x",
		nil,
	)
	req.RemoteAddr = "203.0.113.77:1"
	rec := httptest.NewRecorder()
	plain.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("nil auth first = %d", rec.Code)
	}
}
