package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/renderproxy/internal/rendermetrics"
)

func TestRunServiceServesMetricsUntilContextCanceled(t *testing.T) {
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

	waitForHealthy(t, "http://"+cfg.OpsAddr+"/metrics")

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
