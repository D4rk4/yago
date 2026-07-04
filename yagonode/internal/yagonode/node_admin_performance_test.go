package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
)

func TestPerformanceSourceMapsGateState(t *testing.T) {
	gates := dhtGateStatusSource{
		snapshot: func(context.Context) dhtexchange.GateState {
			return dhtexchange.GateState{
				CrawlQueueSize:   9,
				IndexQueueSize:   4,
				ConnectedPeers:   6,
				LocalRWIWords:    100,
				StorageAvailable: true,
			}
		},
	}

	status := newPerformanceSource(gates).Performance(context.Background())
	if !status.Available {
		t.Fatal("performance status must be available")
	}
	if status.CrawlQueueSize != 9 || status.IndexQueueSize != 4 {
		t.Fatalf("queues = %d/%d", status.CrawlQueueSize, status.IndexQueueSize)
	}
	if status.ConnectedPeers != 6 || status.LocalRWIWords != 100 || !status.StorageAvailable {
		t.Fatalf("status = %+v", status)
	}
}
