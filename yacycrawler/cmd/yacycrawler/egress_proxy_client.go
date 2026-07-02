package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

var errRedirectLimitReached = errors.New("redirect limit reached")

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

func newEgressProxyClient(
	proxyURL *url.URL,
	config CrawlConfig,
) *http.Client {
	dialer := &net.Dialer{Timeout: config.ConnectTimeout}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	transport.DialContext = dialer.DialContext
	transport.TLSHandshakeTimeout = config.TLSTimeout
	transport.ResponseHeaderTimeout = config.HeaderTimeout
	return &http.Client{
		Timeout:       config.RequestTimeout,
		Transport:     transport,
		CheckRedirect: limitedRedirectPolicy(config.MaxRedirects),
	}
}

func limitedRedirectPolicy(maxRedirects int) func(*http.Request, []*http.Request) error {
	return func(_ *http.Request, previous []*http.Request) error {
		if len(previous) > maxRedirects {
			return fmt.Errorf(
				"%w: attempted %d redirects, limit %d",
				errRedirectLimitReached,
				len(previous),
				maxRedirects,
			)
		}
		return nil
	}
}
