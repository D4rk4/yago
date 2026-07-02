package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/D4rk4/yago/yacyegress"
)

var errRedirectLimitReached = errors.New("redirect limit reached")

func newGuardedEgressClient(guard yacyegress.Guard, config CrawlConfig) *http.Client {
	dialer := &net.Dialer{Timeout: config.ConnectTimeout, Control: guard.DialControl}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
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
