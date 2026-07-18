package publicweb

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"

	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagoegress"
)

type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

type AdmissionFetcher struct {
	inner    pagefetch.PageSource
	resolver Resolver
	guard    yagoegress.Guard
}

func NewAdmissionFetcher(
	inner pagefetch.PageSource,
	resolver Resolver,
	guard yagoegress.Guard,
) *AdmissionFetcher {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &AdmissionFetcher{inner: inner, resolver: resolver, guard: guard}
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
		return f.admitAddress(addr)
	}

	addresses, err := f.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return fmt.Errorf("resolve host: %w: %w", pagefetch.ErrPageRejected, err)
	}
	if len(addresses) == 0 {
		return reject("empty resolution")
	}
	dialable := 0
	for _, address := range addresses {
		// A bogus unspecified record (":: " or "0.0.0.0" riding along real
		// addresses, often published to "disable" IPv6) cannot be dialed, so
		// it neither admits nor condemns the host; the dial-time guard stays
		// as the second line. Private and other non-public addresses still
		// reject the whole host to keep the DNS-rebinding defense.
		if address.Unmap().IsUnspecified() {
			continue
		}
		if err := f.admitAddress(address); err != nil {
			return err
		}
		dialable++
	}
	if dialable == 0 {
		return reject("no dialable address")
	}
	return nil
}

func (f *AdmissionFetcher) admitAddress(addr netip.Addr) error {
	if err := f.guard.AdmitAddr(addr); err != nil {
		return markPermanentAdmissionFailure(
			fmt.Errorf("%w: %w", err, pagefetch.ErrPageRejected),
		)
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

func reject(reason string) error {
	return markPermanentAdmissionFailure(
		fmt.Errorf("%s: %w", reason, pagefetch.ErrPageRejected),
	)
}
