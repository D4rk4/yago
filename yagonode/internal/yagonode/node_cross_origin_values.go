package yagonode

import (
	"fmt"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

const (
	maximumCrossOriginConfigurationBytes = 8 << 10
	maximumCrossOrigins                  = 64
)

func parseCrossOriginList(raw string) ([]string, error) {
	if len(raw) > maximumCrossOriginConfigurationBytes || strings.ContainsAny(raw, "\x00\r\n") {
		return nil, fmt.Errorf(
			"origin list must contain at most %d bytes on one line",
			maximumCrossOriginConfigurationBytes,
		)
	}

	items := splitList(raw)
	if len(items) > maximumCrossOrigins {
		return nil, fmt.Errorf("origin list must contain at most %d origins", maximumCrossOrigins)
	}
	origins := make([]string, 0, len(items))
	seen := make(map[string]struct{})
	wildcard := false
	for _, item := range items {
		origin, err := canonicalCrossOrigin(item)
		if err != nil {
			return nil, err
		}
		if origin == "*" {
			wildcard = true
			continue
		}
		if _, duplicate := seen[origin]; duplicate {
			continue
		}
		seen[origin] = struct{}{}
		origins = append(origins, origin)
	}
	if wildcard {
		return []string{"*"}, nil
	}
	sort.Strings(origins)
	if len(origins) == 0 {
		return nil, nil
	}

	return origins, nil
}

func normalizeCrossOriginList(raw string) (string, error) {
	origins, err := parseCrossOriginList(raw)
	if err != nil {
		return "", err
	}

	return strings.Join(origins, ","), nil
}

func canonicalCrossOrigin(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "*" {
		return value, nil
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("invalid origin %q: %w", value, err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if err := validateCrossOriginURL(parsed, scheme, value); err != nil {
		return "", err
	}

	host, ipv6, err := canonicalCrossOriginHost(parsed.Hostname())
	if err != nil {
		return "", fmt.Errorf("invalid origin %q: %w", value, err)
	}
	port, err := canonicalCrossOriginPort(scheme, parsed.Port(), value)
	if err != nil {
		return "", err
	}

	return formatCrossOrigin(scheme, host, ipv6, port), nil
}

func validateCrossOriginURL(parsed *url.URL, scheme, value string) error {
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("origin %q must use http or https", value)
	}
	if parsed.Host == "" || parsed.User != nil || parsed.Opaque != "" ||
		(parsed.Path != "" && parsed.Path != "/") ||
		(parsed.RawPath != "" && parsed.RawPath != "/") ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" ||
		strings.ContainsAny(value, "?#") || strings.HasSuffix(parsed.Host, ":") {
		return fmt.Errorf("origin %q must contain only scheme, host, and optional port", value)
	}

	return nil
}

func canonicalCrossOriginPort(scheme, port, value string) (string, error) {
	if port == "" {
		return "", nil
	}
	parsedPort, err := strconv.ParseUint(port, 10, 16)
	if err != nil || parsedPort == 0 {
		return "", fmt.Errorf("origin %q has an invalid port", value)
	}
	if (scheme == "http" && parsedPort == 80) ||
		(scheme == "https" && parsedPort == 443) {
		return "", nil
	}

	return port, nil
}

func formatCrossOrigin(scheme, host string, ipv6 bool, port string) string {
	if port != "" {
		host = "[" + host + "]"
		if !ipv6 {
			host = strings.Trim(host, "[]")
		}

		return scheme + "://" + host + ":" + port
	}
	if ipv6 {
		host = "[" + host + "]"
	}

	return scheme + "://" + host
}

func canonicalCrossOriginHost(raw string) (string, bool, error) {
	if raw == "" {
		return "", false, fmt.Errorf("host is empty")
	}
	if address, err := netip.ParseAddr(raw); err == nil {
		address = address.Unmap()

		return address.String(), address.Is6(), nil
	}
	host, err := idna.Lookup.ToASCII(raw)
	if err != nil {
		return "", false, fmt.Errorf("convert origin host to ASCII: %w", err)
	}

	return strings.ToLower(host), false, nil
}
