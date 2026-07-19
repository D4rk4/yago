package remotecrawl

import (
	"context"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

type destinationCandidate struct {
	url  *url.URL
	host string
}

func parseDestinationCandidate(rawURL string) (destinationCandidate, error) {
	if len(rawURL) == 0 || len(rawURL) > MaximumReceiptURLBytes {
		return destinationCandidate{}, fmt.Errorf("remote crawl URL length is outside policy")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" || parsed.Host == "" {
		return destinationCandidate{}, fmt.Errorf(
			"remote crawl URL is not an absolute hierarchical URL",
		)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return destinationCandidate{}, fmt.Errorf("remote crawl URL scheme is not allowed")
	}
	if parsed.User != nil {
		return destinationCandidate{}, fmt.Errorf("remote crawl URL credentials are not allowed")
	}
	if err := validateDestinationPort(parsed); err != nil {
		return destinationCandidate{}, err
	}
	host := normalizeDomain(parsed.Hostname())
	if host == "" || strings.Contains(host, "%") {
		return destinationCandidate{}, fmt.Errorf("remote crawl URL host is invalid")
	}

	return destinationCandidate{url: parsed, host: host}, nil
}

func (p destinationPolicy) admitDestinationHost(
	ctx context.Context,
	host string,
) (string, error) {
	address, err := netip.ParseAddr(host)
	if err == nil {
		return p.admitLiteralAddress(address)
	}

	return host, p.admitDomainAddresses(ctx, host)
}

func (p destinationPolicy) admitLiteralAddress(address netip.Addr) (string, error) {
	address = address.Unmap()
	if !p.prefixAllows(address) {
		return "", fmt.Errorf("remote crawl IP destination is not allowlisted")
	}
	if err := p.guard.AdmitAddr(address); err != nil {
		return "", fmt.Errorf("remote crawl IP destination: %w", err)
	}

	return address.String(), nil
}

func (p destinationPolicy) admitDomainAddresses(ctx context.Context, host string) error {
	if !validDomain(host) {
		return fmt.Errorf("remote crawl domain is invalid")
	}
	addresses, err := p.resolver(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve remote crawl destination: %w", err)
	}
	if len(addresses) == 0 {
		return fmt.Errorf("remote crawl destination resolved without addresses")
	}
	_, domainAllowed := p.domains[host]
	for _, address := range addresses {
		if err := p.admitResolvedAddress(address, domainAllowed); err != nil {
			return err
		}
	}

	return nil
}

func (p destinationPolicy) admitResolvedAddress(
	address netip.Addr,
	domainAllowed bool,
) error {
	address = address.Unmap()
	if !domainAllowed && !p.prefixAllows(address) {
		return fmt.Errorf("remote crawl destination is not allowlisted")
	}
	if err := p.guard.AdmitAddr(address); err != nil {
		return fmt.Errorf("remote crawl resolved destination: %w", err)
	}

	return nil
}
