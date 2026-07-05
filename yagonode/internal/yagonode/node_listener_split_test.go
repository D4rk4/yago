package yagonode

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagoegress"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
)

// TestAssembledNodeSplitsPeerAndPublicSurfaces proves the peer listener carries
// only the /yacy/* wire protocol plus the identity landing page, while the
// client-facing search surfaces live on the separate public listener.
func TestAssembledNodeSplitsPeerAndPublicSurfaces(t *testing.T) {
	config := testConfig(t)
	assembled, err := assembleNode(
		t.Context(),
		config,
		openTestVault(t),
		newGuardedEgressClient(yagoegress.NewGuard(config.EgressAllowLAN)),
		nodeTelemetry{
			dhtOutbound: metrics.NewDHTOutboundMetrics(prometheus.NewRegistry()),
			dhtInbound:  metrics.NewDHTInboundMetrics(prometheus.NewRegistry()),
			toggles:     newRuntimeToggles(config),
		},
	)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}

	if code, body := serveGet(t, assembled.peerMux, "/"); code != http.StatusOK ||
		!strings.Contains(body, "YagoSeek") {
		t.Fatalf("peer root = %d body=%q, want 200 landing", code, body)
	}
	if code, _ := serveGet(
		t,
		assembled.peerMux,
		"/yacysearch.json?query=x",
	); code != http.StatusNotFound {
		t.Fatalf("peer /yacysearch.json = %d, want 404 (moved to public listener)", code)
	}
	if code, _ := serveGet(
		t,
		assembled.publicMux,
		"/yacysearch.json?query=x",
	); code == http.StatusNotFound {
		t.Fatal("public /yacysearch.json = 404, want the search surface served")
	}
	if code, body := serveGet(t, assembled.publicMux, "/"); code != http.StatusOK ||
		!strings.Contains(body, "YagoSeek") {
		t.Fatalf("public root = %d body=%q, want 200 landing", code, body)
	}
}

// TestBuildOpsMuxRedirectsRootToConsole proves the operations listener's root
// redirects to the admin console instead of answering a bare 404.
func TestBuildOpsMuxRedirectsRootToConsole(t *testing.T) {
	config, assembled := crawlEnabledNode(t)
	if assembled.crawl != nil {
		t.Cleanup(assembled.crawl.Close)
	}

	mux := buildOpsMux(
		metrics.NewHTTPEndpointMetrics(),
		config,
		assembled,
		events.NewRecorder(4),
		consoleAdminSources{},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("ops root status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != adminui.BasePath {
		t.Fatalf("ops root Location = %q, want %q", loc, adminui.BasePath)
	}
}

func serveGet(t *testing.T, handler http.Handler, target string) (int, string) {
	t.Helper()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	handler.ServeHTTP(rec, req)

	return rec.Code, rec.Body.String()
}
