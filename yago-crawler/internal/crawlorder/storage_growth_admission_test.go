package crawlorder_test

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlorder"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type rejectingGrowthAdmission struct {
	called chan struct{}
}

func (admission rejectingGrowthAdmission) WaitForGrowth(context.Context) bool {
	close(admission.called)

	return false
}

type unexpectedGrowthAdmission struct{}

func (unexpectedGrowthAdmission) WaitForGrowth(context.Context) bool {
	panic("recovered crawl order reached new-growth admission")
}

type observedRequestExpander struct {
	called chan struct{}
}

func (expander observedRequestExpander) Expand(
	context.Context,
	[]yagocrawlcontract.CrawlRequest,
) ([]yagocrawlcontract.CrawlRequest, error) {
	close(expander.called)

	return nil, nil
}

func TestNewOrderWaitsForStorageBeforeExpansion(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](1)
	crawlFrontier := frontier.NewFrontier(1, nil)
	admissionCalled := make(chan struct{})
	expanderCalled := make(chan struct{})
	consumer := crawlorder.NewCrawlOrderConsumer(
		queue,
		crawlFrontier,
		observedRequestExpander{called: expanderCalled},
	).WithGrowthAdmission(rejectingGrowthAdmission{called: admissionCalled})
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go consumer.Run(ctx)
	order, _ := checkpointOrder(t, "storage-pressure-new")
	if err := queue.Publish(ctx, crawlorder.CrawlOrderDelivery{
		Order: order,
	}); err != nil {
		t.Fatalf("publish crawl order: %v", err)
	}
	select {
	case <-admissionCalled:
	case <-time.After(time.Second):
		t.Fatal("new crawl order did not reach storage admission")
	}
	select {
	case <-expanderCalled:
		t.Fatal("storage-blocked crawl order reached expansion")
	default:
	}
	queue.Close()
}

func TestCompletedCheckpointBypassesNewGrowthAdmission(t *testing.T) {
	checkpoint := openConsumerCheckpoint(t)
	order, identity := checkpointOrder(t, "storage-pressure-recovery")
	stageCompletedCheckpoint(t, checkpoint, order, identity, false)
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](1)
	consumer := crawlorder.NewCrawlOrderConsumer(
		queue,
		frontier.NewFrontier(1, nil, frontier.WithCheckpoint(checkpoint)),
	).WithGrowthAdmission(unexpectedGrowthAdmission{})
	settled := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go consumer.Run(ctx)
	if err := queue.Publish(ctx, crawlorder.CrawlOrderDelivery{
		Order: order,
		Ack: func(context.Context) error {
			close(settled)

			return nil
		},
	}); err != nil {
		t.Fatalf("publish recovered order: %v", err)
	}
	select {
	case <-settled:
	case <-time.After(time.Second):
		t.Fatal("completed checkpoint did not settle")
	}
	queue.Close()
}
