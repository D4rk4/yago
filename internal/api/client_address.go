package api

import (
	"net"
	"net/http"
	"strings"
)

const forwardedForHeader = "X-Forwarded-For"

func clientAddress(r *http.Request, trustedProxies []*net.IPNet) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	peer := net.ParseIP(host)
	if peer != nil && ipInAny(peer, trustedProxies) {
		if forwarded := firstForwarded(r.Header.Get(forwardedForHeader)); forwarded != "" {
			return forwarded
		}
	}

	return host
}

func firstForwarded(header string) string {
	if header == "" {
		return ""
	}

	first, _, _ := strings.Cut(header, ",")

	return strings.TrimSpace(first)
}

func ipInAny(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}

	return false
}
