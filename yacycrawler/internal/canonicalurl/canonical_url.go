package canonicalurl

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
	portHTTP    = "80"
	portHTTPS   = "443"
)

func Canonicalize(rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}
	return canonicalize(parsed)
}

func ResolveReference(base, ref string) (string, error) {
	parsedBase, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return "", fmt.Errorf("parse base url: %w", err)
	}
	parsedRef, err := url.Parse(strings.TrimSpace(ref))
	if err != nil {
		return "", fmt.Errorf("parse reference url: %w", err)
	}
	return canonicalize(parsedBase.ResolveReference(parsedRef))
}

func canonicalize(parsed *url.URL) (string, error) {
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != schemeHTTP && scheme != schemeHTTPS {
		return "", fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return "", fmt.Errorf("missing host in %q", parsed.String())
	}
	port := parsed.Port()
	if (scheme == schemeHTTP && port == portHTTP) || (scheme == schemeHTTPS && port == portHTTPS) {
		port = ""
	}

	parsed.Scheme = scheme
	parsed.Host = host
	if port != "" {
		parsed.Host = host + ":" + port
	}
	parsed.Fragment = ""
	parsed.Path = cleanPath(parsed.Path)

	return parsed.String(), nil
}

func cleanPath(raw string) string {
	if raw == "" {
		return "/"
	}
	trailingSlash := strings.HasSuffix(raw, "/")
	cleaned := path.Clean(raw)
	if cleaned == "." {
		return "/"
	}
	if trailingSlash && !strings.HasSuffix(cleaned, "/") {
		cleaned += "/"
	}
	return cleaned
}
