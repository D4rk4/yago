package httpfetch

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
)

func transportForDialMode(proxyURL *url.URL, dialMode ProxyDialMode) http.RoundTripper {
	if dialMode == ProxyDialAbsoluteURL {
		return &absoluteURLTransport{proxyAddr: proxyURL.Host}
	}
	return &http.Transport{Proxy: http.ProxyURL(proxyURL)}
}

// absoluteURLTransport sends the target as an absolute-URI request line to the
// proxy over a plain connection, for proxies that refuse to tunnel via CONNECT.
type absoluteURLTransport struct {
	proxyAddr string
}

func (t *absoluteURLTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	conn, err := (&net.Dialer{}).DialContext(request.Context(), "tcp", t.proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("dial proxy %s: %w", t.proxyAddr, err)
	}

	if err := request.WriteProxy(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("write request to proxy %s: %w", t.proxyAddr, err)
	}

	response, err := http.ReadResponse(bufio.NewReader(conn), request)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("read response from proxy %s: %w", t.proxyAddr, err)
	}
	response.Body = &connClosingBody{ReadCloser: response.Body, conn: conn}
	return response, nil
}

type connClosingBody struct {
	io.ReadCloser
	conn net.Conn
}

func (b *connClosingBody) Close() error {
	bodyErr := b.ReadCloser.Close()
	connErr := b.conn.Close()
	if bodyErr != nil {
		return fmt.Errorf("close response body: %w", bodyErr)
	}
	if connErr != nil {
		return fmt.Errorf("close proxy connection: %w", connErr)
	}
	return nil
}
