package yagonode

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"github.com/D4rk4/yago/yagoegress"
)

const outboundRequestTimeout = 30 * time.Second

func newRuntimeEgressClient(config nodeConfig) *http.Client {
	return newGuardedEgressClient(yagoegress.NewGuard(
		config.EgressAllowLAN,
		yagoegress.WithPrivateAllowlist(config.EgressAllowedCIDRs),
	))
}

func newGuardedEgressClient(guard yagoegress.Guard) *http.Client {
	return newGuardedEgressClientWithTLS(guard, nil)
}

// newRuntimePeerProtocolClient builds the egress-guarded client for outbound
// YaCy peer-protocol calls when https is preferred. Peers in the wild serve
// self-signed certificates (YaCy installs generate their own), so certificate
// verification is off for THIS client only: peer authenticity on the YaCy wire
// comes from protocol-level checks (target hash, network name, hello magic),
// not PKI, and the transport is still no worse than the plain-http default.
// The egress dial guard applies unchanged.
func newRuntimePeerProtocolClient(config nodeConfig) *http.Client {
	// Certificate verification is deliberately off for the YaCy peer
	// protocol; authenticity is protocol-level, not PKI (see doc comment).
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12} // nosemgrep
	tlsConfig.InsecureSkipVerify = true

	return newGuardedEgressClientWithTLS(
		yagoegress.NewGuard(
			config.EgressAllowLAN,
			yagoegress.WithPrivateAllowlist(config.EgressAllowedCIDRs),
		),
		tlsConfig,
	)
}

func newGuardedEgressClientWithTLS(guard yagoegress.Guard, tlsConfig *tls.Config) *http.Client {
	dialer := &net.Dialer{Control: guard.DialControl}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = yagoegress.PreferIPv4(nil, dialer.DialContext)
	if tlsConfig != nil {
		transport.TLSClientConfig = tlsConfig
	}

	return &http.Client{
		Timeout:   outboundRequestTimeout,
		Transport: userAgentTransport{agent: userAgent, next: transport},
	}
}

// userAgentTransport brands outbound requests with the node's User-Agent so
// peers and origin servers see a yago identity. It only fills the header when a
// caller left it unset, so callers with their own agent (the extract fetcher, the
// web-search provider) keep theirs, and it clones the request before mutating it
// so a caller's header map is never touched.
type userAgentTransport struct {
	agent string
	next  http.RoundTripper
}

func (t userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" {
		req = req.Clone(req.Context())
		req.Header.Set("User-Agent", t.agent)
	}

	//nolint:wrapcheck // transparent transport wrapper: surface the round-trip error unchanged.
	return t.next.RoundTrip(req)
}
