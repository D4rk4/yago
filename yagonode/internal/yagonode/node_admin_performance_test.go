package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
)

func TestPerformanceSourceReadsBrokerAndGate(t *testing.T) {
	gates := dhtGateStatusSource{
		snapshot: func(context.Context) dhtexchange.GateState {
			return dhtexchange.GateState{
				IndexQueueSize:   4,
				ConnectedPeers:   6,
				LocalRWIWords:    100,
				StorageAvailable: true,
			}
		},
	}
	crawl := crawlQueueDepthSource{
		probe: func(context.Context) (crawlbroker.QueueDepth, error) {
			return crawlbroker.QueueDepth{Pending: 7, Leased: 2}, nil
		},
	}

	status := newPerformanceSource(gates, crawl).Performance(context.Background())
	if !status.Available {
		t.Fatal("performance status must be available")
	}
	if status.CrawlQueueSize != 9 || status.IndexQueueSize != 4 {
		t.Fatalf("queues = %d/%d, want 9/4", status.CrawlQueueSize, status.IndexQueueSize)
	}
	if status.ConnectedPeers != 6 || status.LocalRWIWords != 100 || !status.StorageAvailable {
		t.Fatalf("status = %+v", status)
	}
}
