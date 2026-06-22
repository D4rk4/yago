package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/nodestatus"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
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
	handler := logHTTPRequests(instrumentHTTP(http.HandlerFunc(
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

func (s stubReport) Header(context.Context) yacyproto.ResponseHeader {
	return yacyproto.ResponseHeader{Version: "1.83", Uptime: 5}
}

func (s stubReport) SelfSeed(context.Context) yacymodel.Seed { return s.seed }

var _ nodestatus.Report = stubReport{}

func TestRuntimeStatusAdapters(t *testing.T) {
	holder := &reportHolder{report: stubReport{seed: yacymodel.Seed{Hash: "0123456789AB"}}}
	ctx := context.Background()

	if got := (rwiStatus{holder}).Snapshot(ctx); got.Version != "1.83" || got.Uptime != 5 {
		t.Errorf("rwi snapshot = %+v", got)
	}
	if got := (urlmetaStatus{holder}).Snapshot(ctx); got.Uptime != 5 {
		t.Errorf("urlmeta snapshot = %+v", got)
	}
	if got := (searchStatus{holder}).Snapshot(ctx); got.Version != "1.83" {
		t.Errorf("search snapshot = %+v", got)
	}
	if got := (crawlingStatus{holder}).Snapshot(ctx); got.Uptime != 5 {
		t.Errorf("crawling snapshot = %+v", got)
	}

	peer := (peeringStatus{holder: holder, networkName: "freeworld"}).Snapshot(ctx)
	if peer.NetworkName != "freeworld" || peer.Seed.Hash != "0123456789AB" {
		t.Errorf("peering snapshot = %+v", peer)
	}
	if boot := (bootstrapStatus{holder}).Snapshot(ctx); boot.Seed.Hash != "0123456789AB" {
		t.Errorf("bootstrap snapshot = %+v", boot)
	}
}

func TestPublishStorageMetricsAndSweepLoop(t *testing.T) {
	config := testConfig(t)
	vault := openTestVault(t)
	publishStorageMetrics(vault)
	publishStorageMetrics(vault)

	assembled := assembleTestNode(t, config, vault)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runEvictionLoop(ctx, assembled.sweeper)
	sweepOnce(context.Background(), assembled.sweeper)
}
