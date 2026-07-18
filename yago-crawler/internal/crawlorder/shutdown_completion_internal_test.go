package crawlorder

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestRunCompletedDuringShutdownGraceIsAcknowledged(t *testing.T) {
	crawlFrontier := frontier.NewFrontier(1, nil)
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
		crawlFrontier,
	)
	profile := consumerProfile()
	ctx, cancel := context.WithCancel(t.Context())
	var acknowledgements atomic.Int64
	var requeues atomic.Int64
	consumer.accept(ctx, CrawlOrderDelivery{
		LeaseID: "grace-completion-lease",
		Order: yagocrawlcontract.CrawlOrder{
			Provenance: []byte("grace-completion-order"),
			Profile:    profile,
			Requests: []yagocrawlcontract.CrawlRequest{{
				URL:           "https://example.org/grace-completion",
				ProfileHandle: profile.Handle,
			}},
		},
		Ack: func(context.Context) error {
			acknowledgements.Add(1)

			return nil
		},
		Nak: func(context.Context) error {
			requeues.Add(1)

			return nil
		},
	})
	job, ok := crawlFrontier.Take(t.Context())
	if !ok {
		t.Fatal("frontier closed before grace-period job")
	}
	cancel()
	crawlFrontier.Done(job, successfulPageOutcome())
	consumer.WaitForSettlements()
	if acknowledgements.Load() != 1 || requeues.Load() != 0 {
		t.Fatalf(
			"settlement ack/nak = %d/%d, want 1/0",
			acknowledgements.Load(),
			requeues.Load(),
		)
	}
}
