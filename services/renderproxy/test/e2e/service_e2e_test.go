//go:build e2e

package e2e

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/dockernetwork"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/lightpanda"
)

func TestRenderproxyRendersScriptedPageEndToEnd(t *testing.T) {
	ctx := context.Background()

	network := dockernetwork.New(t, ctx)

	originURL := startScriptedOrigin(t, ctx, network.Name)
	lightpanda.Start(t, ctx, network.Name)
	renderproxyURL := startRenderproxy(t, ctx, network.Name)

	client := forwardProxyClient(t, renderproxyURL)
	resp, err := client.Get(originURL)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read proxy response body: %v", err)
	}
	if !strings.Contains(string(body), renderedMarker) {
		t.Fatalf("rendered body missing marker: status=%d body=%q", resp.StatusCode, body)
	}
}

func TestRenderproxyRefusesConnectEndToEnd(t *testing.T) {
	ctx := context.Background()

	network := dockernetwork.New(t, ctx)

	startScriptedOrigin(t, ctx, network.Name)
	lightpanda.Start(t, ctx, network.Name)
	renderproxyURL := startRenderproxy(t, ctx, network.Name)

	status := connectResponseStatus(t, renderproxyURL)
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("connect status = %d, want %d", status, http.StatusMethodNotAllowed)
	}
}

// connectResponseStatus drives a real CONNECT handshake through renderproxy by
// requesting an https:// target through it; net/http issues the CONNECT and hands
// the raw response to OnProxyConnectResponse before deciding whether to tunnel TLS.
func connectResponseStatus(t *testing.T, renderproxyURL string) int {
	t.Helper()
	proxyURL, err := url.Parse(renderproxyURL)
	if err != nil {
		t.Fatalf("parse renderproxy url: %v", err)
	}

	var status int
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
		OnProxyConnectResponse: func(_ context.Context, _ *url.URL, _ *http.Request, connectRes *http.Response) error {
			status = connectRes.StatusCode
			return nil
		},
	}
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}

	// The connect target need not exist; only the intercepted CONNECT status matters.
	resp, err := client.Get("https://example.invalid/")
	if err == nil {
		_ = resp.Body.Close()
	}
	return status
}

func forwardProxyClient(t *testing.T, renderproxyURL string) *http.Client {
	t.Helper()
	proxyURL, err := url.Parse(renderproxyURL)
	if err != nil {
		t.Fatalf("parse renderproxy url: %v", err)
	}
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
	}
}
