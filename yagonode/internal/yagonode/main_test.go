package yagonode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagoegress"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestRunRejectsInvalidConfig(t *testing.T) {
	// An empty hash is now valid (the identity is generated), so use a malformed
	// hash to exercise the configuration-rejection path.
	t.Setenv(envPeerHash, "short")
	if err := run(); err == nil {
		t.Fatal("expected error for invalid config")
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
	})
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

	assembled, err := assembleNode(
		context.Background(),
		config,
		vault,
		newGuardedEgressClient(yagoegress.NewGuard(config.EgressAllowLAN)),
		nodeTelemetry{
			dhtOutbound: metrics.NewDHTOutboundMetrics(prometheus.NewRegistry()),
			dhtInbound:  metrics.NewDHTInboundMetrics(prometheus.NewRegistry()),
		},
	)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}

	return assembled
}

func TestBuildServerBoundsRequestResources(t *testing.T) {
	server := buildServer("127.0.0.1:0", http.NotFoundHandler())
	if server.ReadHeaderTimeout != serverReadHeaderTimeout ||
		server.ReadTimeout != serverReadTimeout ||
		server.IdleTimeout != serverIdleTimeout ||
		server.MaxHeaderBytes != serverMaxHeaderBytes {
		t.Fatalf("server limits = %#v", server)
	}
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
			buildServer(
				"127.0.0.1:0",
				newOpsMux(metrics.NewHTTPEndpointMetrics().Handler(), nil, nil, nil, nil),
			),
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
	newOpsMux(metrics.NewHTTPEndpointMetrics().Handler(), nil, nil, nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", rec.Code)
	}
}

func TestOpsMuxServesReadiness(t *testing.T) {
	rec := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, pathReady, nil)
	newOpsMux(
		metrics.NewHTTPEndpointMetrics().Handler(),
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		}),
		nil,
		nil,
		nil,
	).ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("ready status = %d, want 202", rec.Code)
	}
}

func TestOpsMuxServesDHTGates(t *testing.T) {
	rec := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, pathDHTGates, nil)
	newOpsMux(
		metrics.NewHTTPEndpointMetrics().Handler(),
		nil,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		}),
		nil,
		nil,
	).ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("dht gate status = %d, want 202", rec.Code)
	}
}

func TestOpsMuxServesCompatibility(t *testing.T) {
	rec := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		pathCompatibility,
		nil,
	)
	newOpsMux(metrics.NewHTTPEndpointMetrics().Handler(), nil, nil, nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("compatibility status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestOpsMuxServesIndexStats(t *testing.T) {
	rec := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, pathIndexStats, nil)
	newOpsMux(
		metrics.NewHTTPEndpointMetrics().Handler(),
		nil,
		nil,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		}),
		nil,
	).ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("index stats status = %d, want 202", rec.Code)
	}
}

func TestOpsMuxServesEvents(t *testing.T) {
	rec := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, pathEvents, nil)
	newOpsMux(
		metrics.NewHTTPEndpointMetrics().Handler(),
		nil,
		nil,
		nil,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		}),
	).ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("events status = %d, want 202", rec.Code)
	}
}
