package api

import (
	"net"
	"net/http"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

const (
	defaultMaxBodyBytes   int64 = 4 << 20
	defaultRequestTimeout       = 30 * time.Second
)

type muxOptions struct {
	maxBodyBytes   int64
	requestTimeout time.Duration
	trustedProxies []*net.IPNet
}

type Option func(*muxOptions)

func WithMaxBodyBytes(limit int64) Option {
	return func(o *muxOptions) {
		if limit > 0 {
			o.maxBodyBytes = limit
		}
	}
}

func WithRequestTimeout(timeout time.Duration) Option {
	return func(o *muxOptions) {
		if timeout > 0 {
			o.requestTimeout = timeout
		}
	}
}

func WithTrustedProxies(nets []*net.IPNet) Option {
	return func(o *muxOptions) {
		o.trustedProxies = nets
	}
}

func NewPeerProtocolMux(
	ident contracts.Identity,
	status contracts.RuntimeStatus,
	peers contracts.PeerDirectory,
	rwi contracts.RWIReceiver,
	urls contracts.URLReceiver,
	searcher contracts.Searcher,
	counter contracts.Counter,
	opts ...Option,
) *http.ServeMux {
	options := muxOptions{
		maxBodyBytes:   defaultMaxBodyBytes,
		requestTimeout: defaultRequestTimeout,
	}
	for _, opt := range opts {
		opt(&options)
	}

	guard := requestGuard{
		ident:        ident,
		maxBodyBytes: options.maxBodyBytes,
		timeout:      options.requestTimeout,
	}

	mux := http.NewServeMux()
	mux.Handle(yacyproto.PathHello, newHelloHandler(guard, status, peers, options.trustedProxies))
	mux.Handle(yacyproto.PathTransferRWI, newTransferRWIHandler(guard, status, rwi))
	mux.Handle(yacyproto.PathTransferURL, newTransferURLHandler(guard, status, urls))
	mux.Handle(yacyproto.PathSearch, newSearchHandler(guard, status, searcher))
	mux.Handle(yacyproto.PathQuery, newQueryHandler(guard, status, counter))
	mux.Handle(yacyproto.PathCrawlReceipt, newCrawlReceiptHandler(guard, status))

	return mux
}
