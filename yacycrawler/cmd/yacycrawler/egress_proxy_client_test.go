package main

import (
	"context"
	"errors"
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
	client := newEgressProxyClient(proxyURL, CrawlConfig{
		RequestTimeout: 5 * time.Second,
		ConnectTimeout: 4 * time.Second,
		TLSTimeout:     3 * time.Second,
		HeaderTimeout:  2 * time.Second,
		MaxRedirects:   3,
	})
	if client.Timeout != 5*time.Second {
		t.Errorf("timeout = %v", client.Timeout)
	}
	if client.CheckRedirect == nil {
		t.Fatal("redirect policy is nil")
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T", client.Transport)
	}
	if transport.DialContext == nil {
		t.Fatal("dial context is nil")
	}
	if transport.TLSHandshakeTimeout != 3*time.Second {
		t.Errorf("tls timeout = %v", transport.TLSHandshakeTimeout)
	}
	if transport.ResponseHeaderTimeout != 2*time.Second {
		t.Errorf("header timeout = %v", transport.ResponseHeaderTimeout)
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

func TestLimitedRedirectPolicy(t *testing.T) {
	policy := limitedRedirectPolicy(1)
	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://example.com/next",
		nil,
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := policy(request, []*http.Request{request}); err != nil {
		t.Fatalf("first redirect should be allowed: %v", err)
	}
	if err := policy(
		request,
		[]*http.Request{request, request},
	); !errors.Is(
		err,
		errRedirectLimitReached,
	) {
		t.Fatalf("error = %v, want redirect limit", err)
	}
}

func TestLimitedRedirectPolicyCanBlockFirstRedirect(t *testing.T) {
	policy := limitedRedirectPolicy(0)
	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://example.com/next",
		nil,
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := policy(request, []*http.Request{request}); !errors.Is(err, errRedirectLimitReached) {
		t.Fatalf("error = %v, want redirect limit", err)
	}
}
