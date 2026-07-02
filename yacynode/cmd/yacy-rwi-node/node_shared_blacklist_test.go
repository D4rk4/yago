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

func TestAssembledNodeServesDataDirectorySharedBlacklist(t *testing.T) {
	config := testConfig(t)
	writeSharedBlacklists(t, config.DataDir)
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

	form := yacyproto.ListRequest{
		Column: yacyproto.ListColumnBlack,
		Name:   "url.default.black",
	}.Form()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yacyproto.PathList+"?"+form.Encode(),
		nil,
	)
	assembled.peerMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "example.org/.*\r\nblocked.example/.*\r\n\r\n" {
		t.Fatalf("Body = %q", rec.Body.String())
	}
}

func writeSharedBlacklists(t *testing.T, dataDir string) {
	t.Helper()

	settingsDir := filepath.Join(dataDir, "SETTINGS")
	listsDir := filepath.Join(dataDir, "LISTS")
	if err := os.MkdirAll(settingsDir, 0o700); err != nil {
		t.Fatalf("create settings dir: %v", err)
	}
	if err := os.MkdirAll(listsDir, 0o700); err != nil {
		t.Fatalf("create lists dir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(settingsDir, "yacy.conf"),
		[]byte("BlackLists.Shared=url.default.black\n"),
		0o600,
	); err != nil {
		t.Fatalf("write yacy.conf: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(listsDir, "url.default.black"),
		[]byte("# ignored\nexample.org/.*\nblocked.example/.*\n"),
		0o600,
	); err != nil {
		t.Fatalf("write blacklist: %v", err)
	}
}
