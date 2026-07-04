package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagoegress"
)

func TestNewGuardedEgressClientAppliesTimeoutsAndGuard(t *testing.T) {
	client := newGuardedEgressClient(yagoegress.NewGuard(false), CrawlConfig{
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
	if transport.Proxy != nil {
		t.Error("transport must not carry an external proxy")
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
}

func getThroughClient(t *testing.T, client *http.Client, target string) error {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	response, err := client.Do(request)
	if response != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	return nil
}

func TestGuardedEgressClientBlocksNonPublicDial(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newGuardedEgressClient(yagoegress.NewGuard(false), CrawlConfig{
		RequestTimeout: time.Second,
		MaxRedirects:   1,
	})
	if err := getThroughClient(t, client, server.URL); !errors.Is(err, yagoegress.ErrBlocked) {
		t.Fatalf("error = %v, want ErrBlocked for a loopback target", err)
	}
}

func TestGuardedEgressClientKeepsBlockingLoopbackWhenPrivateAllowed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newGuardedEgressClient(yagoegress.NewGuard(true), CrawlConfig{
		RequestTimeout: time.Second,
		MaxRedirects:   1,
	})
	if err := getThroughClient(t, client, server.URL); !errors.Is(err, yagoegress.ErrBlocked) {
		t.Fatalf("error = %v, want ErrBlocked (loopback stays blocked)", err)
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
	); !errors.Is(err, errRedirectLimitReached) {
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
