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
		Transport: transport,
	}
}
