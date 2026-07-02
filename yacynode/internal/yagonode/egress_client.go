package yagonode

import (
	"net"
	"net/http"
	"time"

	"github.com/D4rk4/yago/yacyegress"
)

const outboundRequestTimeout = 30 * time.Second

func newGuardedEgressClient(guard yacyegress.Guard) *http.Client {
	dialer := &net.Dialer{Control: guard.DialControl}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialer.DialContext

	return &http.Client{
		Timeout:   outboundRequestTimeout,
		Transport: transport,
	}
}
