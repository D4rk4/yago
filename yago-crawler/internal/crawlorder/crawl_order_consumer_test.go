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

func TestConsumerSeedsFrontierAndAcks(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](4)
	f := frontier.NewFrontier(8, nil)
	consumer := crawlorder.NewCrawlOrderConsumer(queue, f)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	profile := yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	order := yagocrawlcontract.CrawlOrder{
		Provenance: []byte("admin"),
		Profile:    profile,
		Requests: []yagocrawlcontract.CrawlRequest{
			{URL: "https://example.com/", ProfileHandle: profile.Handle},
		},
	}
	acked := make(chan struct{})
	delivery := crawlorder.CrawlOrderDelivery{
		Order: order,
		Ack:   func(context.Context) error { close(acked); return nil },
	}
	if err := queue.Publish(ctx, delivery); err != nil {
		t.Fatalf("publish delivery: %v", err)
	}

	takeCtx, cancelTake := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancelTake()
	if job, ok := f.Take(takeCtx); ok {
		if job.URL != "https://example.com/" {
			t.Errorf("seeded job url = %q", job.URL)
		}
		if string(job.Provenance) != "admin" {
			t.Errorf("provenance = %q", job.Provenance)
		}
		if job.ProfileHandle != profile.Handle {
			t.Errorf("profile handle = %q want %q", job.ProfileHandle, profile.Handle)
		}
		f.Done(job, successfulPageOutcome())
	} else {
		t.Fatal("frontier never received seeded job")
	}

	select {
	case <-acked:
	case <-time.After(3 * time.Second):
		t.Fatal("delivery never acked after run finished")
	}
}

func TestConsumerAcksRunWithQuarantinedPageFailure(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](4)
	f := frontier.NewFrontier(8, nil)
	consumer := crawlorder.NewCrawlOrderConsumer(queue, f)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	profile := yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	order := yagocrawlcontract.CrawlOrder{
		Provenance: []byte("admin"),
		Profile:    profile,
		Requests: []yagocrawlcontract.CrawlRequest{
			{URL: "https://example.com/", ProfileHandle: profile.Handle},
		},
	}
	acked := make(chan struct{})
	delivery := crawlorder.CrawlOrderDelivery{
		Order: order,
		Ack:   func(context.Context) error { close(acked); return nil },
		Nak:   func(context.Context) error { t.Error("quarantined page must not requeue the order"); return nil },
	}
	if err := queue.Publish(ctx, delivery); err != nil {
		t.Fatalf("publish delivery: %v", err)
	}

	takeCtx, cancelTake := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancelTake()
	if job, ok := f.Take(takeCtx); ok {
		f.Done(job, failedPageOutcome())
	} else {
		t.Fatal("frontier never received seeded job")
	}

	select {
	case <-acked:
	case <-time.After(3 * time.Second):
		t.Fatal("delivery was not acknowledged after the failed page was quarantined")
	}
}

func TestConsumerTermsUncompilableProfile(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](4)
	f := frontier.NewFrontier(8, nil)
	consumer := crawlorder.NewCrawlOrderConsumer(queue, f)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	order := yagocrawlcontract.CrawlOrder{
		Profile: yagocrawlcontract.CrawlProfile{URLMustMatch: "("},
	}
	termed := make(chan struct{})
	delivery := crawlorder.CrawlOrderDelivery{
		Order: order,
		Term:  func(context.Context) error { close(termed); return nil },
		Ack: func(context.Context) error {
			t.Error("uncompilable profile must not ack")
			return nil
		},
	}
	if err := queue.Publish(ctx, delivery); err != nil {
		t.Fatalf("publish delivery: %v", err)
	}

	select {
	case <-termed:
	case <-time.After(3 * time.Second):
		t.Fatal("uncompilable profile was not termed")
	}
}

func TestConsumerRunReturnsWhenContextIsCanceled(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](1)
	f := frontier.NewFrontier(1, nil)
	consumer := crawlorder.NewCrawlOrderConsumer(queue, f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		consumer.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("consumer did not return after context cancellation")
	}
}

func TestConsumerRunReturnsWhenOrdersClose(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](1)
	f := frontier.NewFrontier(1, nil)
	consumer := crawlorder.NewCrawlOrderConsumer(queue, f)
	queue.Close()

	done := make(chan struct{})
	go func() {
		consumer.Run(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("consumer did not return after order receiver closed")
	}
}
