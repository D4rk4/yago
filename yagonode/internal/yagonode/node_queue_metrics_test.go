package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
)

func TestQueueDepthSourceReadsBrokerAndGate(t *testing.T) {
	gates := dhtGateStatusSource{
		snapshot: func(context.Context) dhtexchange.GateState {
			return dhtexchange.GateState{IndexQueueSize: 3}
		},
	}
	crawl := crawlQueueDepthSource{
		probe: func(context.Context) (crawlbroker.QueueDepth, error) {
			return crawlbroker.QueueDepth{Pending: 5, Leased: 3}, nil
		},
	}

	source := newQueueDepthSource(gates, crawl)
	if got := source.CrawlQueueDepth(context.Background()); got != 8 {
		t.Fatalf("crawl depth = %d, want 8", got)
	}
	if got := source.IndexQueueDepth(context.Background()); got != 3 {
		t.Fatalf("index depth = %d, want 3", got)
	}
}
