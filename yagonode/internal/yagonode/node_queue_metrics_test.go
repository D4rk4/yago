package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
)

func TestQueueDepthSourceMapsGateState(t *testing.T) {
	gates := dhtGateStatusSource{
		snapshot: func(context.Context) dhtexchange.GateState {
			return dhtexchange.GateState{CrawlQueueSize: 8, IndexQueueSize: 3}
		},
	}

	source := newQueueDepthSource(gates)
	if got := source.CrawlQueueDepth(context.Background()); got != 8 {
		t.Fatalf("crawl depth = %d, want 8", got)
	}
	if got := source.IndexQueueDepth(context.Background()); got != 3 {
		t.Fatalf("index depth = %d, want 3", got)
	}
}
