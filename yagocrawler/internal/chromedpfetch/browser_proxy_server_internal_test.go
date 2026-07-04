package chromedpfetch

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestProxyExecAllocatorOptionsPinsProxyServer(t *testing.T) {
	options := proxyExecAllocatorOptions("http://proxy:4750")
	if len(options) != 1 {
		t.Fatalf("options = %d, want 1", len(options))
	}
	if options[0] == nil {
		t.Fatal("proxy option is nil")
	}
}

func TestStartGuardedForwardProxyReportsListenError(t *testing.T) {
	restore := listenBrowserProxy
	t.Cleanup(func() { listenBrowserProxy = restore })
	listenBrowserProxy = func() (net.Listener, error) {
		return nil, errors.New("listen refused")
	}

	if _, err := startGuardedForwardProxy(nil); err == nil {
		t.Fatal("expected listen error")
	}
}

func dialTo(addr string) dialFunc {
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
}

func TestForwardProxyForwardsAbsoluteRequest(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Backend", "yes")
		_, _ = io.WriteString(w, "backend body")
	}))
	defer backend.Close()

	proxy, err := startGuardedForwardProxy(dialTo(backend.Listener.Addr().String()))
	if err != nil {
		t.Fatalf("start proxy: %v", err)
	}
	defer proxy.Close()

	proxyURL, _ := url.Parse(proxy.url)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://example.com/page",
		nil,
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("get through proxy: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	body, _ := io.ReadAll(response.Body)
	if string(body) != "backend body" {
		t.Errorf("body = %q", body)
	}
	if response.Header.Get("X-Backend") != "yes" {
		t.Error("expected backend header to be relayed")
	}
}

func TestForwardProxyReturnsBadGatewayWhenDialFails(t *testing.T) {
	handler := &forwardProxyHandler{
		transport: &http.Transport{
			DialContext: func(context.Context, string, string) (net.Conn, error) {
				return nil, errors.New("dial blocked")
			},
		},
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://example.com/page",
		nil,
	)
	handler.forward(recorder, request)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", recorder.Code)
	}
}

func TestForwardProxyTunnelsConnect(t *testing.T) {
	backend := startEchoServer(t)

	proxy, err := startGuardedForwardProxy(dialTo(backend))
	if err != nil {
		t.Fatalf("start proxy: %v", err)
	}
	defer proxy.Close()

	conn, err := (&net.Dialer{}).DialContext(
		context.Background(),
		"tcp",
		strings.TrimPrefix(proxy.url, "http://"),
	)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	_, _ = io.WriteString(conn, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n")
	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(status, "200") {
		t.Fatalf("connect status = %q err = %v", status, err)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read headers: %v", err)
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}

	_, _ = io.WriteString(conn, "ping")
	echoed := make([]byte, 4)
	if _, err := io.ReadFull(reader, echoed); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(echoed) != "ping" {
		t.Errorf("echo = %q", echoed)
	}
}

func TestTunnelReturnsBadGatewayWhenDialFails(t *testing.T) {
	handler := &forwardProxyHandler{
		dial: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("dial blocked")
		},
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodConnect,
		"http://example.com:443",
		nil,
	)
	handler.tunnel(recorder, request)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", recorder.Code)
	}
}

func TestTunnelReturnsErrorWhenHijackUnsupported(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = server.Close() }()
	handler := &forwardProxyHandler{
		dial: func(context.Context, string, string) (net.Conn, error) {
			return client, nil
		},
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodConnect,
		"http://example.com:443",
		nil,
	)
	handler.tunnel(recorder, request)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
}

type fakeHijacker struct {
	*httptest.ResponseRecorder
	conn net.Conn
	err  error
}

func (f *fakeHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return f.conn, nil, f.err
}

type failWriteConn struct {
	net.Conn
}

func (failWriteConn) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestTunnelReturnsWhenHijackFails(t *testing.T) {
	upstream, peer := net.Pipe()
	defer func() { _ = peer.Close() }()
	handler := &forwardProxyHandler{
		dial: func(context.Context, string, string) (net.Conn, error) {
			return upstream, nil
		},
	}
	writer := &fakeHijacker{ResponseRecorder: httptest.NewRecorder(), err: errors.New("no hijack")}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodConnect,
		"http://example.com:443",
		nil,
	)
	handler.tunnel(writer, request)
}

func TestTunnelReturnsWhenClientWriteFails(t *testing.T) {
	upstream, peer := net.Pipe()
	defer func() { _ = peer.Close() }()
	client, clientPeer := net.Pipe()
	defer func() { _ = clientPeer.Close() }()
	handler := &forwardProxyHandler{
		dial: func(context.Context, string, string) (net.Conn, error) {
			return upstream, nil
		},
	}
	writer := &fakeHijacker{
		ResponseRecorder: httptest.NewRecorder(),
		conn:             failWriteConn{Conn: client},
	}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodConnect,
		"http://example.com:443",
		nil,
	)
	handler.tunnel(writer, request)
}

func startEchoServer(t *testing.T) string {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()

	return listener.Addr().String()
}
