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

var cloneBaseTransport = func() *http.Transport {
	return http.DefaultTransport.(*http.Transport).Clone()
}

func newGuardedEgressClient(guard yagoegress.Guard, config CrawlConfig) *http.Client {
	return newGuardedEgressClientWithTLS(guard, config, nil)
}

// newInsecureEgressClient builds the client behind the crawl profiles that set
// IgnoreTLSAuthority: certificate-chain verification is off for operators who
// need self-signed or mis-chained sites crawled. The fetched payload is public
// web content, and the egress dial guard applies unchanged.
func newInsecureEgressClient(guard yagoegress.Guard, config CrawlConfig) *http.Client {
	return newGuardedEgressClientWithTLS(guard, config, insecureTLSConfig())
}

// insecureTLSConfig is the shared verification-off TLS setup for the
// IgnoreTLSAuthority crawl profiles.
func insecureTLSConfig() *tls.Config {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12} // nosemgrep
	tlsConfig.InsecureSkipVerify = true

	return tlsConfig
}

func newGuardedEgressClientWithTLS(
	guard yagoegress.Guard,
	config CrawlConfig,
	tlsConfig *tls.Config,
) *http.Client {
	return buildEgressClient(guard, config, tlsConfig, false)
}

// newHTTP1EgressClient builds the same guarded client with HTTP/2 disabled —
// the fallback path for hosts whose bot protection resets Go's h2 streams
// (CRAWL-18). Everything else (dial guard, timeouts, redirects) is identical.
func newHTTP1EgressClient(
	guard yagoegress.Guard,
	config CrawlConfig,
	tlsConfig *tls.Config,
) *http.Client {
	return buildEgressClient(guard, config, tlsConfig, true)
}

func buildEgressClient(
	guard yagoegress.Guard,
	config CrawlConfig,
	tlsConfig *tls.Config,
	http1Only bool,
) *http.Client {
	dialer := &net.Dialer{Timeout: config.ConnectTimeout, Control: guard.DialControl}
	transport := cloneBaseTransport()
	transport.Proxy = nil
	transport.DialContext = yagoegress.PreferIPv4(nil, dialer.DialContext)
	transport.TLSHandshakeTimeout = config.TLSTimeout
	transport.ResponseHeaderTimeout = config.HeaderTimeout
	if tlsConfig != nil {
		transport.TLSClientConfig = tlsConfig.Clone()
	}
	if http1Only {
		transport.ForceAttemptHTTP2 = false
		transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		transport.TLSClientConfig.NextProtos = []string{"http/1.1"}
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
