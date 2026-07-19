package yagonode

import (
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maximumPeerNameBytes              = 256
	maximumGreetsPerCycle             = 1024
	maximumSeedlistConfigurationBytes = 8 << 10
	maximumSeedlistURLs               = 64
)

func parsePeerName(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if len(value) > maximumPeerNameBytes || !utf8.ValidString(value) ||
		strings.ContainsAny(value, "\x00\r\n") {
		return "", fmt.Errorf(
			"peer name must be valid UTF-8 on one line and contain at most %d bytes",
			maximumPeerNameBytes,
		)
	}

	return value, nil
}

func parseAdvertiseHost(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		value = strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
	}
	if strings.ContainsAny(value, "\x00\r\n/?#@%") {
		return "", fmt.Errorf("advertised host must contain only a DNS name or IP address")
	}
	if address, err := netip.ParseAddr(value); err == nil {
		return address.Unmap().String(), nil
	}
	if strings.Contains(value, ":") {
		return "", fmt.Errorf("advertised host must not include a port")
	}
	host, _, err := canonicalCrossOriginHost(value)
	if err != nil || len(host) > 253 {
		return "", fmt.Errorf("advertised host is invalid")
	}

	return host, nil
}

func parseSeedlistURLs(raw string) ([]string, error) {
	if len(raw) > maximumSeedlistConfigurationBytes || strings.ContainsAny(raw, "\x00\r\n") {
		return nil, fmt.Errorf(
			"seedlist URL list must contain at most %d bytes on one line",
			maximumSeedlistConfigurationBytes,
		)
	}
	items := splitList(raw)
	if len(items) > maximumSeedlistURLs {
		return nil, fmt.Errorf(
			"seedlist URL list must contain at most %d URLs",
			maximumSeedlistURLs,
		)
	}
	values := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		value, err := canonicalSeedlistURL(item)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	if len(values) == 0 {
		return nil, nil
	}

	return values, nil
}

func normalizeSeedlistURLs(raw string) (string, error) {
	values, err := parseSeedlistURLs(raw)
	if err != nil {
		return "", err
	}

	return strings.Join(values, ","), nil
}

func canonicalSeedlistURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("invalid seedlist URL %q: %w", value, err)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("seedlist URL %q must use http or https", value)
	}
	if parsed.Host == "" || parsed.User != nil || parsed.Opaque != "" ||
		parsed.Fragment != "" || strings.Contains(value, "#") {
		return "", fmt.Errorf(
			"seedlist URL %q must be an absolute URL without credentials or a fragment",
			value,
		)
	}
	host, ipv6, err := canonicalCrossOriginHost(parsed.Hostname())
	if err != nil {
		return "", fmt.Errorf("invalid seedlist URL %q: %w", value, err)
	}
	port := parsed.Port()
	if port != "" {
		parsedPort, err := strconv.ParseUint(port, 10, 16)
		if err != nil || parsedPort == 0 {
			return "", fmt.Errorf("seedlist URL %q has an invalid port", value)
		}
		if parsed.Scheme == "http" && parsedPort == 80 ||
			parsed.Scheme == "https" && parsedPort == 443 {
			port = ""
		}
	}
	if ipv6 {
		host = "[" + host + "]"
	}
	parsed.Host = host
	if port != "" {
		parsed.Host += ":" + port
	}

	return parsed.String(), nil
}
