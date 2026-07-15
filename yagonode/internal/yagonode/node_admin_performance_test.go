package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
)

func TestPerformanceSourceReadsBrokerAndGate(t *testing.T) {
	gates := dhtGateStatusSource{
		snapshot: func(context.Context) dhtexchange.GateState {
			return dhtexchange.GateState{
				CrawlQueueSize:   9,
				CrawlQueueKnown:  true,
				IndexQueueSize:   4,
				IndexQueueKnown:  true,
				ConnectedPeers:   6,
				LocalRWIWords:    100,
				LocalRWIKnown:    true,
				StorageAvailable: true,
				StorageKnown:     true,
			}
		},
	}

	status := newPerformanceSource(gates).Performance(context.Background())
	if !status.Available {
		t.Fatal("performance status must be available")
	}
	if status.CrawlQueueSize != 9 || status.IndexQueueSize != 4 {
		t.Fatalf("queues = %d/%d, want 9/4", status.CrawlQueueSize, status.IndexQueueSize)
	}
	if !status.CrawlQueueKnown || !status.IndexQueueKnown ||
		status.ConnectedPeers != 6 || status.LocalRWIWords != 100 ||
		!status.LocalRWIKnown || !status.StorageAvailable || !status.StorageKnown {
		t.Fatalf("status = %+v", status)
	}
}

func TestPerformanceSourcePreservesUnknownMeasurements(t *testing.T) {
	status := newPerformanceSource(dhtGateStatusSource{
		snapshot: func(context.Context) dhtexchange.GateState {
			return dhtexchange.GateState{ConnectedPeers: 6}
		},
	}).Performance(t.Context())
	if !status.Available || status.CrawlQueueKnown || status.IndexQueueKnown ||
		status.LocalRWIKnown || status.StorageKnown {
		t.Fatalf("status = %+v", status)
	}

	unavailable := newPerformanceSource(dhtGateStatusSource{}).Performance(t.Context())
	if unavailable.Available {
		t.Fatalf("nil snapshot status = %+v", unavailable)
	}
}
