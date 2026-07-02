package publicweb

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"

	"github.com/D4rk4/yago/yacycrawler/internal/pagefetch"
)

type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

type AdmissionFetcher struct {
	inner    pagefetch.PageSource
	resolver Resolver
}

var nonPublicAddressPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

func NewAdmissionFetcher(inner pagefetch.PageSource, resolver Resolver) *AdmissionFetcher {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &AdmissionFetcher{inner: inner, resolver: resolver}
}

func (f *AdmissionFetcher) Fetch(
	ctx context.Context,
	target *url.URL,
) (pagefetch.FetchedPage, error) {
	if err := f.admit(ctx, target); err != nil {
		return pagefetch.FetchedPage{}, fmt.Errorf("target: %w", err)
	}
	page, err := f.inner.Fetch(ctx, target)
	if err != nil {
		return pagefetch.FetchedPage{}, fmt.Errorf("inner fetch: %w", err)
	}
	if err := f.admit(ctx, page.URL); err != nil {
		return pagefetch.FetchedPage{}, fmt.Errorf("final target: %w", err)
	}
	return page, nil
}

func (f *AdmissionFetcher) admit(ctx context.Context, target *url.URL) error {
	if target == nil || target.Host == "" {
		return reject("missing host")
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return reject("unsupported scheme")
	}

	host := target.Hostname()
	if host == "" {
		return reject("missing host")
	}
	if localHostName(host) {
		return reject("local host")
	}
	if addr, ok := parseHostAddress(host); ok {
		return admitAddress(addr)
	}

	addresses, err := f.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return fmt.Errorf("resolve host %s: %w", err.Error(), pagefetch.ErrPageRejected)
	}
	if len(addresses) == 0 {
		return reject("empty resolution")
	}
	for _, address := range addresses {
		if err := admitAddress(address); err != nil {
			return err
		}
	}
	return nil
}

func parseHostAddress(host string) (netip.Addr, bool) {
	candidate := host
	if value, _, ok := strings.Cut(candidate, "%"); ok {
		candidate = value
	}
	addr, err := netip.ParseAddr(candidate)
	return addr, err == nil
}

func localHostName(host string) bool {
	normalized := strings.TrimSuffix(strings.ToLower(host), ".")
	return normalized == "localhost" || strings.HasSuffix(normalized, ".localhost")
}

func admitAddress(addr netip.Addr) error {
	addr = addr.Unmap()
	if !addr.IsValid() {
		return reject("invalid address")
	}
	if addr.IsLoopback() ||
		addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() ||
		addr.IsUnspecified() {
		return reject("non-public address")
	}
	for _, prefix := range nonPublicAddressPrefixes {
		if prefix.Contains(addr) {
			return reject("non-public address")
		}
	}
	return nil
}

func reject(reason string) error {
	return fmt.Errorf("%s: %w", reason, pagefetch.ErrPageRejected)
}
