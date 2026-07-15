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
			return dhtexchange.GateState{IndexQueueSize: 3, IndexQueueKnown: true}
		},
	}
	crawl := crawlQueueDepthSource{
		probe: func(context.Context) (crawlbroker.QueueDepth, error) {
			return crawlbroker.QueueDepth{Pending: 5, Leased: 3}, nil
		},
	}

	source := newQueueDepthSource(gates, crawl)
	if got, known := source.CrawlQueueDepth(context.Background()); got != 8 || !known {
		t.Fatalf("crawl depth = %d known=%t, want 8 true", got, known)
	}
	if got, known := source.IndexQueueDepth(context.Background()); got != 3 || !known {
		t.Fatalf("index depth = %d known=%t, want 3 true", got, known)
	}
}

func TestQueueDepthSourcePreservesUnknownObservations(t *testing.T) {
	source := newQueueDepthSource(
		dhtGateStatusSource{snapshot: func(context.Context) dhtexchange.GateState {
			return dhtexchange.GateState{}
		}},
		crawlQueueDepthSource{probe: func(context.Context) (crawlbroker.QueueDepth, error) {
			return crawlbroker.QueueDepth{}, context.Canceled
		}},
	)

	if depth, known := source.CrawlQueueDepth(context.Background()); depth != 0 || known {
		t.Fatalf("crawl observation = %d known=%t, want 0 false", depth, known)
	}
	if depth, known := source.IndexQueueDepth(context.Background()); depth != 0 || known {
		t.Fatalf("index observation = %d known=%t, want 0 false", depth, known)
	}
}
