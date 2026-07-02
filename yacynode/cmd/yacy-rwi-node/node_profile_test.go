package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yacynode/internal/metrics"
	"github.com/D4rk4/yago/yacyproto"
)

func TestAssembledNodeServesDataDirectoryProfile(t *testing.T) {
	config := testConfig(t)
	writePeerProfile(t, config.DataDir, "operator=alice\nstatement=hello\\nworld\n")
	assembled, err := assembleNode(
		t.Context(),
		config,
		openTestVault(t),
		newEgressProxyClient(config.ProxyURL, outboundRequestTimeout),
		nodeTelemetry{
			dhtOutbound: metrics.NewDHTOutboundMetrics(prometheus.NewRegistry()),
			dhtInbound:  metrics.NewDHTInboundMetrics(prometheus.NewRegistry()),
		},
	)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}

	form := yacyproto.ProfileRequest{NetworkName: "freeworld"}.Form()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yacyproto.PathProfile+"?"+form.Encode(),
		nil,
	)
	assembled.peerMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "operator=alice\r\nstatement=hello\\nworld\r\n" {
		t.Fatalf("Body = %q", rec.Body.String())
	}
}

func writePeerProfile(t *testing.T, dataDir, body string) {
	t.Helper()

	settingsDir := filepath.Join(dataDir, "SETTINGS")
	if err := os.MkdirAll(settingsDir, 0o700); err != nil {
		t.Fatalf("create profile dir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(settingsDir, "profile.txt"),
		[]byte(body),
		0o600,
	); err != nil {
		t.Fatalf("write profile: %v", err)
	}
}
