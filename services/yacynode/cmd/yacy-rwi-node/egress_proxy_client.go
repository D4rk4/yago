package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	envProxyURL = "YACY_PROXY_URL"

	outboundRequestTimeout = 30 * time.Second
)

func egressProxyURL(getenv func(string) string) (*url.URL, error) {
	raw := strings.TrimSpace(getenv(envProxyURL))
	if raw == "" {
		return nil, fmt.Errorf("%s: must be set", envProxyURL)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", envProxyURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("%s: scheme must be http or https", envProxyURL)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("%s: must include a host", envProxyURL)
	}

	return parsed, nil
}

func newEgressProxyClient(proxyURL *url.URL, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
	}
}
