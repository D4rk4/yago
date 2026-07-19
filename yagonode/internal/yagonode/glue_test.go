package yagonode

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/nodestatus"
)

func TestConfigureLogging(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	var output bytes.Buffer
	if err := configureLoggingTo(func(string) string { return "debug" }, &output); err != nil {
		t.Fatalf("configure: %v", err)
	}
	slog.InfoContext(t.Context(), "stdout logging probe")
	if !strings.Contains(output.String(), `"msg":"stdout logging probe"`) {
		t.Fatalf("configured output = %q", output.String())
	}
	if err := configureLoggingTo(func(string) string { return "nonsense" }, &output); err == nil {
		t.Fatal("expected error for invalid level")
	}
}

func TestMiddlewareRecordsStatus(t *testing.T) {
	handler := logHTTPRequests(instrumentHTTP(metrics.NewHTTPEndpointMetrics(), http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		},
	)))

	rec := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/x", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestMiddlewareRecordsSuccess(t *testing.T) {
	handler := logHTTPRequests(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
	))

	rec := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/x", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func TestStatusRecorderKeepsFirstStatus(t *testing.T) {
	rec := &statusRecorder{ResponseWriter: httptest.NewRecorder(), status: http.StatusOK}
	rec.WriteHeader(http.StatusTeapot)
	rec.WriteHeader(http.StatusInternalServerError)

	if rec.status != http.StatusTeapot {
		t.Fatalf("status = %d, want first write 418", rec.status)
	}
}

type stubReport struct{ seed yagomodel.Seed }

func (s stubReport) Version(context.Context) string { return "1.83" }

func (s stubReport) Uptime(context.Context) int { return 5 }

func (s stubReport) UptimeSeconds(context.Context) int { return 315 }

func (s stubReport) SelfSeed(context.Context) yagomodel.Seed { return s.seed }

var _ nodestatus.Report = stubReport{}

func TestRuntimeStatusAdapters(t *testing.T) {
	report := stubReport{seed: yagomodel.Seed{Hash: "0123456789AB"}}
	ctx := context.Background()

	peer := peeringStatus{report: report, networkName: "freeworld"}
	if got := peer.NetworkName(ctx); got != "freeworld" {
		t.Errorf("peering network = %q", got)
	}
	if got := peer.SelfSeed(ctx); got.Hash != "0123456789AB" {
		t.Errorf("peering self seed = %+v", got)
	}
}

func TestPublishStorageMetricsAndSweepLoop(t *testing.T) {
	config := testConfig(t)
	vault := openTestVault(t)
	metrics.NewStorageMetrics(prometheus.NewRegistry(), vault)

	assembled := assembleTestNode(t, config, vault)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	observer := metrics.NewEvictionMetrics(prometheus.NewRegistry())
	runEvictionLoop(ctx, assembled.sweeper, observer)
	sweepOnce(context.Background(), assembled.sweeper, observer)
}
