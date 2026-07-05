package yagonode

import (
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
	dialer := &net.Dialer{Control: guard.DialControl}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialer.DialContext

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
