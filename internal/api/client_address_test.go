package api

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientAddressUntrustedPeerIgnoresForwarded(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	r.RemoteAddr = "198.51.100.7:9000"
	r.Header.Set(forwardedForHeader, "203.0.113.5")

	_, trusted, _ := net.ParseCIDR("192.0.2.0/24")
	if got := clientAddress(r, []*net.IPNet{trusted}); got != "198.51.100.7" {
		t.Errorf("clientAddress = %q, want untrusted remote", got)
	}
}

func TestClientAddressTrustedPeerHonorsForwarded(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	r.RemoteAddr = "192.0.2.10:9000"
	r.Header.Set(forwardedForHeader, "203.0.113.5, 192.0.2.10")

	_, trusted, _ := net.ParseCIDR("192.0.2.0/24")
	if got := clientAddress(r, []*net.IPNet{trusted}); got != "203.0.113.5" {
		t.Errorf("clientAddress = %q, want forwarded address", got)
	}
}

func TestClientAddressNoTrustedProxies(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	r.RemoteAddr = "192.0.2.10:9000"
	r.Header.Set(forwardedForHeader, "203.0.113.5")

	if got := clientAddress(r, nil); got != "192.0.2.10" {
		t.Errorf("clientAddress = %q, want remote address", got)
	}
}
