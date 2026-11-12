package publicratelimit

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/httpguard"
)

func TestLimiterUsesForwardedAddressOnlyFromTrustedProxy(t *testing.T) {
	_, trustedProxy, err := net.ParseCIDR("192.0.2.10/32")
	if err != nil {
		t.Fatal(err)
	}
	limiter := NewLimiter(
		Tiers{Per3Seconds: 1, PerMinute: 10, Per10Minutes: 10},
		httpguard.NewClientAddressResolver([]*net.IPNet{trustedProxy}),
	)
	trustedClientA := newLimitedRequest("192.0.2.10:8080", "198.51.100.21")
	trustedClientB := newLimitedRequest("192.0.2.10:8080", "198.51.100.22")
	if allowed, _ := limiter.AllowRequest(trustedClientA, false); !allowed {
		t.Fatal("first trusted client request was refused")
	}
	if allowed, _ := limiter.AllowRequest(trustedClientB, false); !allowed {
		t.Fatal("second client behind trusted proxy shared the first client's limit")
	}

	untrustedClient := newLimitedRequest("203.0.113.9:8080", "198.51.100.23")
	if allowed, _ := limiter.AllowRequest(untrustedClient, false); !allowed {
		t.Fatal("first untrusted proxy request was refused")
	}
	untrustedSpoof := newLimitedRequest("203.0.113.9:8080", "198.51.100.24")
	if allowed, _ := limiter.AllowRequest(untrustedSpoof, false); allowed {
		t.Fatal("untrusted forwarded address changed the client's rate-limit bucket")
	}
}

func newLimitedRequest(remoteAddress, forwardedAddress string) *http.Request {
	request := httptest.NewRequest("POST", "/search", nil)
	request.RemoteAddr = remoteAddress
	request.Header.Set("X-Forwarded-For", forwardedAddress)

	return request
}
