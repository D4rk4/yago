package adminauth

import (
	"net"
	"net/http"
	"strings"
)

type trustedProxyRequestPolicy struct {
	trustedProxies []*net.IPNet
}

func (p trustedProxyRequestPolicy) ClientAddress(r *http.Request) string {
	remoteAddress := clientIP(r)
	peer := net.ParseIP(remoteAddress)
	if peer == nil || !p.trusted(peer) {
		return remoteAddress
	}

	forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	if len(forwarded) == 1 && strings.TrimSpace(forwarded[0]) == "" {
		return remoteAddress
	}
	addresses := make([]net.IP, 0, len(forwarded))
	for _, value := range forwarded {
		address := net.ParseIP(strings.TrimSpace(value))
		if address == nil {
			return remoteAddress
		}
		addresses = append(addresses, address)
	}

	current := peer
	for index := len(addresses) - 1; index >= 0; index-- {
		if !p.trusted(current) {
			return current.String()
		}
		current = addresses[index]
	}

	return current.String()
}

func (p trustedProxyRequestPolicy) Secure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	peer := net.ParseIP(clientIP(r))
	if peer == nil || !p.trusted(peer) {
		return false
	}

	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func (p trustedProxyRequestPolicy) trusted(address net.IP) bool {
	for _, network := range p.trustedProxies {
		if network != nil && network.Contains(address) {
			return true
		}
	}

	return false
}
