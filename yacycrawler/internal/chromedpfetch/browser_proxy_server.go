package chromedpfetch

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/chromedp/chromedp"
)

const (
	browserProxyReadHeaderTimeout = 10 * time.Second
	browserProxyShutdownTimeout   = 5 * time.Second
)

var listenBrowserProxy = func() (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:0")
}

func proxyExecAllocatorOptions(proxyURL string) []chromedp.ExecAllocatorOption {
	return []chromedp.ExecAllocatorOption{chromedp.ProxyServer(proxyURL)}
}

type dialFunc func(ctx context.Context, network, address string) (net.Conn, error)

type guardedForwardProxy struct {
	server *http.Server
	url    string
}

func startGuardedForwardProxy(dial dialFunc) (*guardedForwardProxy, error) {
	listener, err := listenBrowserProxy()
	if err != nil {
		return nil, fmt.Errorf("listen browser proxy: %w", err)
	}
	proxy := &guardedForwardProxy{
		server: &http.Server{
			Handler: &forwardProxyHandler{
				dial:      dial,
				transport: &http.Transport{DialContext: dial},
			},
			ReadHeaderTimeout: browserProxyReadHeaderTimeout,
		},
		url: "http://" + listener.Addr().String(),
	}
	go func() { _ = proxy.server.Serve(listener) }()

	return proxy, nil
}

func (p *guardedForwardProxy) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), browserProxyShutdownTimeout)
	defer cancel()
	_ = p.server.Shutdown(ctx)
}

type forwardProxyHandler struct {
	dial      dialFunc
	transport *http.Transport
}

func (h *forwardProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		h.tunnel(w, r)

		return
	}
	h.forward(w, r)
}

func (h *forwardProxyHandler) tunnel(w http.ResponseWriter, r *http.Request) {
	upstream, err := h.dial(r.Context(), "tcp", r.Host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)

		return
	}
	defer func() { _ = upstream.Close() }()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "connection hijack unsupported", http.StatusInternalServerError)

		return
	}
	client, _, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer func() { _ = client.Close() }()
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	relay(client, upstream)
}

func relay(client, upstream net.Conn) {
	done := make(chan struct{}, 2)
	go copyConn(client, upstream, done)
	go copyConn(upstream, client, done)
	<-done
}

func copyConn(dst, src net.Conn, done chan<- struct{}) {
	_, _ = io.Copy(dst, src)
	done <- struct{}{}
}

func (h *forwardProxyHandler) forward(w http.ResponseWriter, r *http.Request) {
	outbound := r.Clone(r.Context())
	outbound.RequestURI = ""
	response, err := h.transport.RoundTrip(outbound)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)

		return
	}
	defer func() { _ = response.Body.Close() }()
	for key, values := range response.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}
