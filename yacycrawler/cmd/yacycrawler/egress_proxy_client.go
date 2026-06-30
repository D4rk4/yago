package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func egressProxyURL(getenv func(string) string) (*url.URL, error) {
	raw := strings.TrimSpace(getenv(EnvProxyURL))
	if raw == "" {
		return nil, fmt.Errorf("%s: must be set", EnvProxyURL)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", EnvProxyURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("%s: scheme must be http or https", EnvProxyURL)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("%s: must include a host", EnvProxyURL)
	}
	return parsed, nil
}

func newEgressProxyClient(proxyURL *url.URL, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
	}
}
