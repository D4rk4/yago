package yagonode

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/publicratelimit"
)

func TestTavilySearchAdmissionUsesAuthenticatedRate(t *testing.T) {
	limiter := publicratelimit.NewLimiter(publicratelimit.Tiers{
		Per3Seconds:  1,
		PerMinute:    100,
		Per10Minutes: 100,
	}, httpguard.NewClientAddressResolver(nil))
	admission := newTavilySearchAdmission(limiter)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/search", nil)
	req.RemoteAddr = "203.0.113.15:1"
	for i := 0; i < 10; i++ {
		release, status, _ := admission(req, true)
		if status != 0 || release == nil {
			t.Fatalf("authenticated admission %d = %d", i, status)
		}
		release()
	}
	if release, status, retryAfter := admission(req, true); release != nil ||
		status != http.StatusTooManyRequests || retryAfter != 3*time.Second {
		t.Fatalf("rate admission = %t %d %v", release != nil, status, retryAfter)
	}
}

func TestTavilySearchAdmissionUsesAnonymousRateWithoutBearer(t *testing.T) {
	limiter := publicratelimit.NewLimiter(publicratelimit.Tiers{
		Per3Seconds:  1,
		PerMinute:    100,
		Per10Minutes: 100,
	}, httpguard.NewClientAddressResolver(nil))
	admission := newTavilySearchAdmission(limiter)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/search", nil)
	request.RemoteAddr = "203.0.113.17:1"
	first, status, _ := admission(request, false)
	if status != 0 || first == nil {
		t.Fatalf("first anonymous admission = %v %d", first != nil, status)
	}
	first()
	if release, status, retryAfter := admission(request, false); release != nil ||
		status != http.StatusTooManyRequests || retryAfter != 3*time.Second {
		t.Fatalf("anonymous rate admission = %t %d %v", release != nil, status, retryAfter)
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
	limiter := publicratelimit.NewLimiter(
		publicratelimit.DefaultPublicTiers(),
		httpguard.NewClientAddressResolver(nil),
	)
	admission := newTavilySearchAdmission(limiter)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/search", nil)
	req.RemoteAddr = "203.0.113.16:1"
	if release, status, retryAfter := admission(req, true); release != nil ||
		status != http.StatusServiceUnavailable || retryAfter != time.Second {
		t.Fatalf("capacity admission = %t %d %v", release != nil, status, retryAfter)
	}
}
