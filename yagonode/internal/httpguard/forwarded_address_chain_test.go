package httpguard

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestForwardedChainStopsAtNearestUntrustedAddress(t *testing.T) {
	_, network, err := net.ParseCIDR("10.0.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewClientAddressResolver([]*net.IPNet{network})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.5:1234"
	request.Header.Add("X-Forwarded-For", "198.51.100.9")
	request.Header.Add("X-Forwarded-For", "203.0.113.8, 10.0.0.6")
	if got := resolver.Resolve(request); got != "203.0.113.8" {
		t.Fatalf("address = %q, want nearest untrusted hop", got)
	}
	request.RemoteAddr = "192.0.2.4:1234"
	if got := resolver.Resolve(request); got != "192.0.2.4" {
		t.Fatalf("untrusted socket peer supplied address %q", got)
	}
}

func TestForwardedChainDirectConnectionIgnoresForwarded(t *testing.T) {
	_, network, err := net.ParseCIDR("10.0.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewClientAddressResolver([]*net.IPNet{network})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	request.RemoteAddr = "192.0.2.4:1234"
	request.Header.Set(forwardedForHeader, "203.0.113.8")

	if got := resolver.Resolve(request); got != "192.0.2.4" {
		t.Fatalf("direct connection returned %q, want socket peer", got)
	}
}

func TestForwardedChainMissingHeaderReturnsSocket(t *testing.T) {
	_, network, err := net.ParseCIDR("10.0.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewClientAddressResolver([]*net.IPNet{network})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.5:1234"

	if got := resolver.Resolve(request); got != "10.0.0.5" {
		t.Fatalf("missing header returned %q, want socket peer", got)
	}
}

func TestForwardedChainStopsAtFirstUntrustedHop(t *testing.T) {
	_, network, err := net.ParseCIDR("10.0.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewClientAddressResolver([]*net.IPNet{network})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.5:1234"
	request.Header.Set(forwardedForHeader, "10.0.0.6, 10.0.0.7, 198.51.100.7")

	if got := resolver.Resolve(request); got != "198.51.100.7" {
		t.Fatalf("address = %q, want nearest untrusted hop", got)
	}
}

func TestForwardedChainAllTrustedReturnsFarthest(t *testing.T) {
	_, network, err := net.ParseCIDR("10.0.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewClientAddressResolver([]*net.IPNet{network})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.5:1234"
	request.Header.Set(forwardedForHeader, "10.0.0.6, 10.0.0.7")

	if got := resolver.Resolve(request); got != "10.0.0.6" {
		t.Fatalf("all-trusted chain returned %q, want farthest parsed hop", got)
	}
}

func TestForwardedChainMalformedMiddleStops(t *testing.T) {
	_, network, err := net.ParseCIDR("10.0.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewClientAddressResolver([]*net.IPNet{network})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.5:1234"
	request.Header.Set(forwardedForHeader, "10.0.0.6, not-an-ip, 10.0.0.7")

	if got := resolver.Resolve(request); got != "10.0.0.7" {
		t.Fatalf("malformed middle returned %q, want last verified hop", got)
	}
}

func TestForwardedChainMalformedRightmostStops(t *testing.T) {
	_, network, err := net.ParseCIDR("10.0.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewClientAddressResolver([]*net.IPNet{network})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.5:1234"
	request.Header.Set(forwardedForHeader, "203.0.113.8, not-an-ip")

	if got := resolver.Resolve(request); got != "10.0.0.5" {
		t.Fatalf("malformed rightmost returned %q, want socket peer", got)
	}
}

func TestForwardedChainEmptyTokenStops(t *testing.T) {
	_, network, err := net.ParseCIDR("10.0.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewClientAddressResolver([]*net.IPNet{network})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.5:1234"
	request.Header.Set(forwardedForHeader, "10.0.0.6, , 10.0.0.7")

	if got := resolver.Resolve(request); got != "10.0.0.7" {
		t.Fatalf("empty token returned %q, want last verified hop", got)
	}
}

func TestForwardedChainIPv6MappedNormalized(t *testing.T) {
	_, network, err := net.ParseCIDR("10.0.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewClientAddressResolver([]*net.IPNet{network})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	request.RemoteAddr = "[::ffff:10.0.0.5]:1234"
	request.Header.Set(forwardedForHeader, "::ffff:10.0.0.6, 198.51.100.7")

	if got := resolver.Resolve(request); got != "198.51.100.7" {
		t.Fatalf("mapped chain returned %q, want nearest untrusted hop", got)
	}
}

func TestForwardedChainZoneIdentifierRejected(t *testing.T) {
	_, network, err := net.ParseCIDR("10.0.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewClientAddressResolver([]*net.IPNet{network})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.5:1234"
	request.Header.Set(forwardedForHeader, "198.51.100.7, fe80::1%eth0")

	if got := resolver.Resolve(request); got != "10.0.0.5" {
		t.Fatalf("zone identifier returned %q, want socket peer", got)
	}
}

func TestForwardedChainNoPortSocket(t *testing.T) {
	_, network, err := net.ParseCIDR("10.0.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewClientAddressResolver([]*net.IPNet{network})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.5"
	request.Header.Set(forwardedForHeader, "198.51.100.7")

	if got := resolver.Resolve(request); got != "198.51.100.7" {
		t.Fatalf("no-port socket returned %q, want nearest untrusted hop", got)
	}
}

func TestForwardedChainUnparsableSocketReturnsSocket(t *testing.T) {
	_, network, err := net.ParseCIDR("10.0.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewClientAddressResolver([]*net.IPNet{network})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	request.RemoteAddr = "not-an-address:1234"

	if got := resolver.Resolve(request); got != "not-an-address" {
		t.Fatalf("unparsable socket returned %q, want host as given", got)
	}
}

func TestForwardedChainSubnetEdge(t *testing.T) {
	_, network, err := net.ParseCIDR("10.0.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewClientAddressResolver([]*net.IPNet{network})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.1.5:1234"
	request.Header.Set(forwardedForHeader, "10.0.0.6, 198.51.100.7")

	if got := resolver.Resolve(request); got != "10.0.1.5" {
		t.Fatalf("out-of-range socket returned %q, want socket peer", got)
	}
}
