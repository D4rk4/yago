package yagonode

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/publicratelimit"
)

func TestTavilySearchAdmissionUsesAuthenticatedRate(t *testing.T) {
	limiter := publicratelimit.NewLimiter(publicratelimit.Tiers{
		Per3Seconds:  1,
		PerMinute:    100,
		Per10Minutes: 100,
	})
	admission := newTavilySearchAdmission(limiter)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/search", nil)
	req.RemoteAddr = "203.0.113.15:1"
	for i := 0; i < 10; i++ {
		release, status, _ := admission(req)
		if status != 0 || release == nil {
			t.Fatalf("authenticated admission %d = %d", i, status)
		}
		release()
	}
	if release, status, retryAfter := admission(req); release != nil ||
		status != http.StatusTooManyRequests || retryAfter != 3*time.Second {
		t.Fatalf("rate admission = %t %d %v", release != nil, status, retryAfter)
	}
}

func TestTavilySearchAdmissionSharesCapacity(t *testing.T) {
	var releases []func()
	for {
		release, admitted := publicratelimit.AdmitSearch()
		if !admitted {
			break
		}
		releases = append(releases, release)
	}
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	if len(releases) == 0 {
		t.Fatal("capacity was unavailable before the test")
	}
	limiter := publicratelimit.NewLimiter(publicratelimit.DefaultPublicTiers())
	admission := newTavilySearchAdmission(limiter)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/search", nil)
	req.RemoteAddr = "203.0.113.16:1"
	if release, status, retryAfter := admission(req); release != nil ||
		status != http.StatusServiceUnavailable || retryAfter != time.Second {
		t.Fatalf("capacity admission = %t %d %v", release != nil, status, retryAfter)
	}
}
