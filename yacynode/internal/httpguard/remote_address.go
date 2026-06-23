package httpguard

import (
	"context"
	"net"
	"net/http"
	"strings"
)

const forwardedForHeader = "X-Forwarded-For"

type remoteAddrKey struct{}

func WithRemoteAddr(ctx context.Context, addr string) context.Context {
	return context.WithValue(ctx, remoteAddrKey{}, addr)
}

func RemoteAddr(ctx context.Context) string {
	addr, _ := ctx.Value(remoteAddrKey{}).(string)

	return addr
}

type ClientAddressResolver struct {
	trustedProxies []*net.IPNet
}

func NewClientAddressResolver(trustedProxies []*net.IPNet) ClientAddressResolver {
	return ClientAddressResolver{trustedProxies: trustedProxies}
}

func (c ClientAddressResolver) Resolve(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	peer := net.ParseIP(host)
	if peer != nil && ipInAny(peer, c.trustedProxies) {
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
