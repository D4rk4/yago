package main

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestNewEgressProxyClientPinsProxy(t *testing.T) {
	proxyURL, err := url.Parse("http://proxy:4750")
	if err != nil {
		t.Fatalf("parse proxy: %v", err)
	}
	client := newEgressProxyClient(proxyURL, 5*time.Second)
	if client.Timeout != 5*time.Second {
		t.Errorf("timeout = %v", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T", client.Transport)
	}
	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://example.com/",
		nil,
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resolved, err := transport.Proxy(request)
	if err != nil {
		t.Fatalf("proxy: %v", err)
	}
	if resolved.String() != "http://proxy:4750" {
		t.Errorf("proxy = %v", resolved)
	}
}
