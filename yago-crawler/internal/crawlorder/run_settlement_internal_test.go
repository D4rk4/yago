package crawlorder

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
)

func TestFinishRunRequeuesUnsuccessfulRun(t *testing.T) {
	crawlFrontier := frontier.NewFrontier(1, nil)
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
		crawlFrontier,
	)
	order := identityTestOrder()
	naked := make(chan struct{})
	delivery := CrawlOrderDelivery{
		LeaseID: "failed-run-lease",
		Order:   order,
		Nak: func(context.Context) error {
			close(naked)

			return nil
		},
	}
	if claim := consumer.active.claim(order.Provenance, delivery); claim != activeOrderStartsRun {
		t.Fatalf("claim = %d, want start", claim)
	}
	reporter := newRunProgressReporter()
	crawlFrontier.Hold()
	consumer.finishRun(t.Context(), order, delivery, reporter)(false)
	waitCallback(t, naked)
}
