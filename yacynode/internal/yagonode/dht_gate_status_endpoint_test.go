package yagonode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/D4rk4/yago/yacynode/internal/dhtexchange"
)

func TestDHTGateStatusEndpointReturnsReport(t *testing.T) {
	source := dhtGateStatusSource{
		snapshot: func(context.Context) dhtexchange.GateState {
			return dhtexchange.GateState{
				PublicReachable:  false,
				LocalPeerKnown:   true,
				ConnectedPeers:   2,
				LocalRWIWords:    5,
				StorageAvailable: true,
			}
		},
		config: dhtexchange.GateConfig{
			NetworkDHTEnabled:    true,
			DistributionEnabled:  true,
			AllowWhileIndexing:   true,
			MinimumConnectedPeer: 3,
			MinimumRWIWord:       4,
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathDHTGates, nil)
	newDHTGateStatusEndpoint(source).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
	}

	var got dhtGateStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Open ||
		got.BlockingReason != dhtexchange.GatePublicReachabilityReason ||
		got.State.ConnectedPeers != 2 ||
		got.Config.MinimumConnectedPeer != 3 ||
		len(got.Gates) != 11 {
		t.Fatalf("response = %#v", got)
	}
	if got.Gates[1].Name != string(dhtexchange.GatePublicReachability) ||
		got.Gates[1].Open ||
		got.Gates[1].Reason != dhtexchange.GatePublicReachabilityReason {
		t.Fatalf("public reachability gate = %#v", got.Gates[1])
	}
}

func TestDHTGateStatusEndpointRejectsNonGET(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, pathDHTGates, nil)
	newDHTGateStatusEndpoint(dhtGateStatusSource{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if rec.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("Allow = %q", rec.Header().Get("Allow"))
	}
}

func TestDHTGateStatusSourceUsesZeroStateWithoutSnapshot(t *testing.T) {
	got := (dhtGateStatusSource{}).response(t.Context())
	if got.Open || got.State.PublicReachable || len(got.Gates) != 11 {
		t.Fatalf("response = %#v", got)
	}
}
