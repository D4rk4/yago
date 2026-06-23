package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/nodestatus"
)

func TestConfigureLogging(t *testing.T) {
	if err := configureLogging(func(string) string { return "debug" }); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if err := configureLogging(func(string) string { return "nonsense" }); err == nil {
		t.Fatal("expected error for invalid level")
	}
}

func TestMiddlewareRecordsStatus(t *testing.T) {
	handler := logHTTPRequests(instrumentHTTP(newEndpointMetrics(), http.HandlerFunc(
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

func TestStatusRecorderKeepsFirstStatus(t *testing.T) {
	rec := &statusRecorder{ResponseWriter: httptest.NewRecorder(), status: http.StatusOK}
	rec.WriteHeader(http.StatusTeapot)
	rec.WriteHeader(http.StatusInternalServerError)

	if rec.status != http.StatusTeapot {
		t.Fatalf("status = %d, want first write 418", rec.status)
	}
}

type stubReport struct{ seed yacymodel.Seed }

func (s stubReport) Version(context.Context) string { return "1.83" }

func (s stubReport) Uptime(context.Context) int { return 5 }

func (s stubReport) SelfSeed(context.Context) yacymodel.Seed { return s.seed }

var _ nodestatus.Report = stubReport{}

func TestRuntimeStatusAdapters(t *testing.T) {
	report := stubReport{seed: yacymodel.Seed{Hash: "0123456789AB"}}
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
	publishStorageMetrics(prometheus.NewRegistry(), vault)

	assembled := assembleTestNode(t, config, vault)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runEvictionLoop(ctx, assembled.sweeper)
	sweepOnce(context.Background(), assembled.sweeper)
}
