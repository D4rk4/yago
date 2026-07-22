package yagonode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
)

func TestDHTGateStatusEndpointReturnsReport(t *testing.T) {
	source := dhtGateStatusSource{
		snapshot: func(context.Context) dhtexchange.GateState {
			return dhtexchange.GateState{
				LocalPeerKnown:   true,
				ConnectedPeers:   2,
				LocalRWIWords:    5,
				LocalRWIKnown:    true,
				CrawlQueueSize:   7,
				CrawlQueueKnown:  true,
				IndexQueueSize:   3,
				IndexQueueKnown:  true,
				StorageAvailable: true,
				StorageKnown:     true,
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
		got.BlockingReason != dhtexchange.GateNetworkTooSmallReason ||
		got.State.PublicReachabilityKnown ||
		got.State.ConnectedPeers != 2 ||
		got.State.CrawlQueueSize != 7 || !got.State.CrawlQueueKnown ||
		got.State.IndexQueueSize != 3 || !got.State.IndexQueueKnown ||
		!got.State.LocalRWIKnown || !got.State.StorageKnown ||
		got.Config.MinimumConnectedPeer != 3 ||
		len(got.Gates) != 10 {
		t.Fatalf("response = %#v", got)
	}
	for _, gate := range got.Gates {
		if gate.Name == "public_reachability" {
			t.Fatalf("public reachability remained an outbound gate: %#v", got.Gates)
		}
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
	if got.Open || got.State.PublicReachable || got.State.PublicReachabilityKnown ||
		len(got.Gates) != 10 {
		t.Fatalf("response = %#v", got)
	}
}

func TestDHTGateStatusSourceKeepsEnrichedReachabilitySnapshot(t *testing.T) {
	want := publicReachabilitySnapshot{
		state: publicReachabilityReachable, source: publicReachabilitySourcePeerBackPing,
		observedAt: time.Unix(12, 0),
	}
	got := (dhtGateStatusSource{
		snapshotWithReachability: func(context.Context) (
			dhtexchange.GateState,
			publicReachabilitySnapshot,
		) {
			return dhtexchange.GateState{}, want
		},
	}).response(t.Context())
	if !got.State.PublicReachable || !got.State.PublicReachabilityKnown ||
		got.State.PublicReachabilitySource != adminui.PublicReachabilityPeerBackPing ||
		got.State.PublicReachabilityObservedAt != "1970-01-01T00:00:12Z" {
		t.Fatalf("enriched reachability = %+v", got)
	}
}

func TestDHTGateStatusKeepsPublicReachabilityOutsideOutboundDecision(t *testing.T) {
	t.Parallel()

	config := dhtexchange.DefaultGateConfig()
	config.MinimumConnectedPeer = 1
	config.MinimumRWIWord = 1
	state := dhtexchange.GateState{
		LocalPeerKnown:   true,
		ConnectedPeers:   1,
		LocalRWIWords:    1,
		LocalRWIKnown:    true,
		CrawlQueueKnown:  true,
		IndexQueueKnown:  true,
		StorageAvailable: true,
		StorageKnown:     true,
	}
	tests := []struct {
		name      string
		snapshot  publicReachabilitySnapshot
		known     bool
		reachable bool
	}{
		{name: "unconfirmed", snapshot: publicReachabilitySnapshot{}},
		{
			name: "unreachable",
			snapshot: publicReachabilitySnapshot{
				state: publicReachabilityUnreachable,
			},
			known: true,
		},
		{
			name: "reachable",
			snapshot: publicReachabilitySnapshot{
				state: publicReachabilityReachable,
			},
			known: true, reachable: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := (dhtGateStatusSource{
				snapshotWithReachability: func(context.Context) (
					dhtexchange.GateState,
					publicReachabilitySnapshot,
				) {
					return state, test.snapshot
				},
				config: config,
			}).response(t.Context())
			if !got.Open || got.BlockingReason != "" ||
				got.State.PublicReachabilityKnown != test.known ||
				got.State.PublicReachable != test.reachable {
				t.Fatalf("status = %+v", got)
			}
			for _, gate := range got.Gates {
				if gate.Name == "public_reachability" {
					t.Fatalf("public reachability remained an outbound gate: %#v", got.Gates)
				}
			}
		})
	}
}
