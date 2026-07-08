package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/renderproxy/internal/rendermetrics"
)

func TestRunServiceServesHealthUntilContextCanceled(t *testing.T) {
	opsListener := reservePort(t)
	cfg := ServiceConfig{
		ListenAddr:        reservePort(t).String(),
		CDPURL:            "http://127.0.0.1:1",
		RenderConcurrency: 1,
		RequestDeadline:   time.Second,
		MaxResponseBytes:  1024,
		OpsAddr:           opsListener.String(),
	}

	ctx, cancel := context.WithCancel(t.Context())
	serviceDone := make(chan error, 1)
	go func() { serviceDone <- RunService(ctx, cfg, rendermetrics.New()) }()

	waitForHealthy(t, "http://"+cfg.OpsAddr+pathHealth)

	cancel()
	select {
	case err := <-serviceDone:
		if err != nil {
			t.Fatalf("RunService: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunService did not stop after context cancel")
	}
}

func reservePort(t *testing.T) *net.TCPAddr {
	t.Helper()
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := listener.Addr().(*net.TCPAddr)
	_ = listener.Close()
	return addr
}

func waitForHealthy(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("build health request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("ops server never became healthy at %s", url)
}

func TestRunServersStopsOnServerError(t *testing.T) {
	occupied := reservePort(t)
	var listenConfig net.ListenConfig
	blocker, err := listenConfig.Listen(t.Context(), "tcp", occupied.String())
	if err != nil {
		t.Fatalf("hold port: %v", err)
	}
	t.Cleanup(func() { _ = blocker.Close() })

	const readHeaderLimit = 10 * time.Second
	conflicting := &http.Server{Addr: occupied.String(), ReadHeaderTimeout: readHeaderLimit}
	unused := &http.Server{Addr: reservePort(t).String(), ReadHeaderTimeout: readHeaderLimit}
	t.Cleanup(func() { _ = unused.Close() })

	err = runServers(t.Context(), conflicting, unused)
	if err == nil {
		t.Fatal("expected error from address already in use")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("expected a bind error, got context cancellation: %v", err)
	}
}
