package yagonode

import (
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode"
)

const maximumPublicSelfTestURLBytes = 2048

func normalizeOptionalPublicSelfTestURL(raw string) (string, error) {
	if len(raw) > maximumPublicSelfTestURLBytes ||
		strings.IndexFunc(raw, unsafePublicSelfTestURLRune) >= 0 {
		return "", fmt.Errorf(
			"public peer self-test URL must be one line of at most %d bytes",
			maximumPublicSelfTestURLBytes,
		)
	}
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	parsed, err := parsePublicSelfTestURL(value)
	if err != nil {
		return "", err
	}
	host, err := canonicalPublicSelfTestAuthority(parsed)
	if err != nil {
		return "", err
	}
	canonical := &url.URL{
		Scheme: strings.ToLower(parsed.Scheme),
		Host:   host,
		Path:   canonicalPublicSelfTestPath(parsed.Path),
	}

	return canonical.String(), nil
}

func unsafePublicSelfTestURLRune(value rune) bool {
	return unicode.IsControl(value) || unicode.Is(unicode.Cf, value)
}

func parsePublicSelfTestURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("invalid public peer self-test URL: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("public peer self-test URL scheme must be http or https")
	}
	if parsed.Host == "" || parsed.Opaque != "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.ForceQuery || strings.Contains(value, "?") ||
		parsed.Fragment != "" || parsed.RawFragment != "" || strings.Contains(value, "#") ||
		strings.HasSuffix(parsed.Host, ":") {
		return nil, fmt.Errorf(
			"public peer self-test URL must contain only scheme, host, optional port, and path",
		)
	}

	return parsed, nil
}

func canonicalPublicSelfTestAuthority(parsed *url.URL) (string, error) {
	host, ipv6, err := canonicalCrossOriginHost(parsed.Hostname())
	if err != nil {
		return "", fmt.Errorf("invalid public peer self-test URL host: %w", err)
	}
	port := parsed.Port()
	if port != "" {
		parsedPort, parseErr := strconv.ParseUint(port, 10, 16)
		if parseErr != nil || parsedPort == 0 {
			return "", fmt.Errorf("public peer self-test URL port is invalid")
		}
		scheme := strings.ToLower(parsed.Scheme)
		if (scheme == "http" && parsedPort == 80) || (scheme == "https" && parsedPort == 443) {
			port = ""
		}
	}
	if ipv6 {
		host = "[" + host + "]"
	}
	if port != "" {
		host += ":" + port
	}

	return host, nil
}

func canonicalPublicSelfTestPath(value string) string {
	basePath := path.Clean(value)
	if basePath == "." || basePath == "/" {
		return ""
	}

	return basePath
}
