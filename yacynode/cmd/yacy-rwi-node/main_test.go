package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
)

func TestRunRejectsInvalidConfig(t *testing.T) {
	t.Setenv(envPeerHash, "")
	if err := run(); err == nil {
		t.Fatal("expected error for missing config")
	}
}

func testConfig(t *testing.T) nodeConfig {
	t.Helper()

	config, err := loadNodeConfig(func(key string) string {
		switch key {
		case envPeerHash:
			return "0123456789AB"
		case envPeerName:
			return "node"
		case envAdvertiseHost:
			return "203.0.113.1"
		case envDataDir:
			return t.TempDir()
		default:
			return ""
		}
	}, false)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	return config
}

func openTestVault(t *testing.T) *boltvault.Vault {
	t.Helper()

	vault, err := boltvault.Open(filepath.Join(t.TempDir(), "db"), 0)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { _ = vault.Close() })

	return vault
}

func assembleTestNode(t *testing.T, config nodeConfig, vault *boltvault.Vault) node {
	t.Helper()

	settings, err := loadBootstrapSettings(func(string) string { return "" })
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	assembled, err := assembleNode(config, settings, vault, newOutboundHTTPClient())
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}

	return assembled
}

func TestServeReturnsNilAfterCancel(t *testing.T) {
	assembled := assembleTestNode(t, testConfig(t), openTestVault(t))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := serve(ctx, assembled,
		namedServer{"peer protocol", buildServer("127.0.0.1:0", assembled.peerMux)},
		namedServer{"ops", buildServer("127.0.0.1:0", newOpsMux(newEndpointMetrics().handler()))},
	)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
}

func TestServeShutsDownOnListenError(t *testing.T) {
	assembled := assembleTestNode(t, testConfig(t), openTestVault(t))

	err := serve(context.Background(), assembled,
		namedServer{"peer protocol", buildServer("203.0.113.255:-1", assembled.peerMux)},
	)
	if err == nil {
		t.Fatal("expected listen error")
	}
}

func TestOpsMuxAnswersHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, pathHealth, nil)
	newOpsMux(newEndpointMetrics().handler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", rec.Code)
	}
}
