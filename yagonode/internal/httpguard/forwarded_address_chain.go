package httpguard

import (
	"net"
	"net/netip"
	"strings"
)

func resolveForwardedChain(
	current netip.Addr,
	headerValues []string,
	trustedProxies []*net.IPNet,
) netip.Addr {
	for i := len(headerValues) - 1; i >= 0; i-- {
		tokens := strings.Split(headerValues[i], ",")
		for j := len(tokens) - 1; j >= 0; j-- {
			if !isTrusted(current, trustedProxies) {
				return current
			}
			next, ok := parseUnscopedAddr(tokens[j])
			if !ok {
				return current
			}
			current = next
		}
	}

	return current
}

func isTrusted(ip netip.Addr, nets []*net.IPNet) bool {
	raw := net.IP(ip.AsSlice())
	for _, n := range nets {
		if n.Contains(raw) {
			return true
		}
	}

	return false
}

func parseUnscopedAddr(token string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(strings.TrimSpace(token))
	if err != nil {
		return netip.Addr{}, false
	}

	if addr.Zone() != "" {
		return netip.Addr{}, false
	}

	return addr.Unmap(), true
}
