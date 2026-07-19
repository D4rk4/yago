package remotecrawl

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagoegress"
)

type HostResolver func(context.Context, string) ([]netip.Addr, error)

type destinationPolicy struct {
	domains  map[string]struct{}
	prefixes []netip.Prefix
	resolver HostResolver
	guard    yagoegress.Guard
}

func newDestinationPolicy(entries []string, resolver HostResolver) (destinationPolicy, error) {
	entries, err := NormalizeAllowedDestinations(entries)
	if err != nil {
		return destinationPolicy{}, err
	}
	domains := map[string]struct{}{}
	var prefixes []netip.Prefix
	for _, entry := range entries {
		if prefix, parseErr := netip.ParsePrefix(entry); parseErr == nil {
			prefixes = append(prefixes, prefix.Masked())
			continue
		}
		domains[entry] = struct{}{}
	}
	if resolver == nil {
		resolver = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}
	}

	return destinationPolicy{
		domains:  domains,
		prefixes: prefixes,
		resolver: resolver,
		guard: yagoegress.NewGuard(
			false,
			yagoegress.WithPrivateAllowlist(prefixes),
		),
	}, nil
}

func NormalizeAllowedDestinations(entries []string) ([]string, error) {
	if len(entries) > MaximumAllowedDestinations {
		return nil, fmt.Errorf(
			"remote crawl destinations must not exceed %d",
			MaximumAllowedDestinations,
		)
	}
	normalized := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			prefix = prefix.Masked()
			if prefix.Bits() == 0 {
				return nil, fmt.Errorf(
					"remote crawl destination prefix must be narrower than an entire address family",
				)
			}
			entry = prefix.String()
		} else {
			entry = normalizeDomain(entry)
			if !validDomain(entry) {
				return nil, fmt.Errorf("invalid remote crawl destination %q", raw)
			}
		}
		if _, duplicate := seen[entry]; duplicate {
			continue
		}
		seen[entry] = struct{}{}
		normalized = append(normalized, entry)
	}
	if len(normalized) == 0 {
		return nil, fmt.Errorf("remote crawl requires a non-empty destination allowlist")
	}

	return normalized, nil
}

func (p destinationPolicy) Admit(ctx context.Context, rawURL string) (string, error) {
	candidate, err := parseDestinationCandidate(rawURL)
	if err != nil {
		return "", err
	}
	validationCtx, cancel := context.WithTimeout(ctx, destinationValidationTimeout)
	defer cancel()
	host, err := p.admitDestinationHost(validationCtx, candidate.host)
	if err != nil {
		return "", err
	}
	candidate.url.Fragment = ""
	candidate.url.RawFragment = ""
	candidate.url.Host = canonicalURLHost(host)

	return candidate.url.String(), nil
}

func validateDestinationPort(parsed *url.URL) error {
	port := defaultDestinationPort(parsed.Scheme)
	if parsed.Port() == "" {
		return nil
	}
	value, err := strconv.Atoi(parsed.Port())
	if err != nil || value < 1 || value > 65535 {
		return fmt.Errorf("remote crawl URL port is invalid")
	}
	if value != port {
		return fmt.Errorf("remote crawl URL port is not allowlisted")
	}

	return nil
}

func defaultDestinationPort(scheme string) int {
	if scheme == "https" {
		return 443
	}

	return 80
}

func canonicalURLHost(host string) string {
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}

	return host
}

func (p destinationPolicy) prefixAllows(address netip.Addr) bool {
	for _, prefix := range p.prefixes {
		if prefix.Contains(address) {
			return true
		}
	}

	return false
}

func normalizeDomain(raw string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
}

func validDomain(domain string) bool {
	if domain == "" || len(domain) > 253 || strings.Contains(domain, "..") {
		return false
	}
	labels := strings.Split(domain, ".")
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, symbol := range label {
			if (symbol < 'a' || symbol > 'z') &&
				(symbol < '0' || symbol > '9') && symbol != '-' {
				return false
			}
		}
	}

	return true
}

func minimumDeadline(
	ctx context.Context,
	duration time.Duration,
) (context.Context, context.CancelFunc) {
	if duration <= 0 {
		duration = time.Second
	}
	return context.WithTimeout(ctx, duration)
}
