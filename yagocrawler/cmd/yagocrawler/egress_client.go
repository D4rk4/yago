package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/D4rk4/yago/yagoegress"
)

var errRedirectLimitReached = errors.New("redirect limit reached")

func newGuardedEgressClient(guard yagoegress.Guard, config CrawlConfig) *http.Client {
	return newGuardedEgressClientWithTLS(guard, config, nil)
}

// newInsecureEgressClient builds the client behind the crawl profiles that set
// IgnoreTLSAuthority: certificate-chain verification is off for operators who
// need self-signed or mis-chained sites crawled. The fetched payload is public
// web content, and the egress dial guard applies unchanged.
func newInsecureEgressClient(guard yagoegress.Guard, config CrawlConfig) *http.Client {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12} // nosemgrep
	tlsConfig.InsecureSkipVerify = true

	return newGuardedEgressClientWithTLS(guard, config, tlsConfig)
}

func newGuardedEgressClientWithTLS(
	guard yagoegress.Guard,
	config CrawlConfig,
	tlsConfig *tls.Config,
) *http.Client {
	dialer := &net.Dialer{Timeout: config.ConnectTimeout, Control: guard.DialControl}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = yagoegress.PreferIPv4(nil, dialer.DialContext)
	transport.TLSHandshakeTimeout = config.TLSTimeout
	transport.ResponseHeaderTimeout = config.HeaderTimeout
	if tlsConfig != nil {
		transport.TLSClientConfig = tlsConfig
	}

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
