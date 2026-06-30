package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/memvault"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/metrics"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
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
		case envProxyURL:
			return "http://proxy:4750"
		default:
			return ""
		}
	}, false)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	return config
}

func openTestVault(t *testing.T) *vault.Vault {
	t.Helper()

	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })

	return v
}

func assembleTestNode(t *testing.T, config nodeConfig, vault *vault.Vault) node {
	t.Helper()

	settings, err := loadBootstrapSettings(func(string) string { return "" })
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	assembled, err := assembleNode(
		context.Background(),
		config,
		settings,
		vault,
		newEgressProxyClient(config.ProxyURL, outboundRequestTimeout),
	)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}

	return assembled
}

func TestServeReturnsNilAfterCancel(t *testing.T) {
	assembled := assembleTestNode(t, testConfig(t), openTestVault(t))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := serve(
		ctx,
		assembled,
		metrics.NewEvictionMetrics(prometheus.NewRegistry()),
		namedServer{"peer protocol", buildServer("127.0.0.1:0", assembled.peerMux)},
		namedServer{
			"ops",
			buildServer("127.0.0.1:0", newOpsMux(metrics.NewHTTPEndpointMetrics().Handler())),
		},
	)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
}

func TestServeShutsDownOnListenError(t *testing.T) {
	assembled := assembleTestNode(t, testConfig(t), openTestVault(t))

	err := serve(
		context.Background(),
		assembled,
		metrics.NewEvictionMetrics(prometheus.NewRegistry()),
		namedServer{"peer protocol", buildServer("203.0.113.255:-1", assembled.peerMux)},
	)
	if err == nil {
		t.Fatal("expected listen error")
	}
}

func TestOpsMuxAnswersHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, pathHealth, nil)
	newOpsMux(metrics.NewHTTPEndpointMetrics().Handler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", rec.Code)
	}
}
