package crawlorder_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/boundedqueue"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawljob"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlorder"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/frontier"
)

func TestConsumerSeedsFrontierAndAcks(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](4)
	f := frontier.NewFrontier(8, nil)
	consumer := crawlorder.NewCrawlOrderConsumer(queue, f, crawlorder.OrderRedeliveryPolicy{
		AckWait:     2 * time.Hour,
		MaxAttempts: 5,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	profile := yacycrawlcontract.NewCrawlProfile(yacycrawlcontract.CrawlProfile{
		Scope:           yacycrawlcontract.ScopeDomain,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
	})
	order := yacycrawlcontract.CrawlOrder{
		OrderID:    uuid.NewString(),
		Provenance: []byte("admin"),
		Profile:    profile,
		Requests: []yacycrawlcontract.CrawlRequest{
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

	select {
	case job := <-f.Jobs():
		if job.URL != "https://example.com/" {
			t.Errorf("seeded job url = %q", job.URL)
		}
		if string(job.Provenance) != "admin" {
			t.Errorf("provenance = %q", job.Provenance)
		}
		if job.ProfileHandle != profile.Handle {
			t.Errorf("profile handle = %q want %q", job.ProfileHandle, profile.Handle)
		}
		f.Done(job, false)
	case <-time.After(3 * time.Second):
		t.Fatal("frontier never received seeded job")
	}

	select {
	case <-acked:
	case <-time.After(3 * time.Second):
		t.Fatal("delivery never acked after run finished")
	}
}

func TestConsumerNaksWhenRunHasDeliveryFailure(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](4)
	f := frontier.NewFrontier(8, nil)
	consumer := crawlorder.NewCrawlOrderConsumer(queue, f, crawlorder.OrderRedeliveryPolicy{
		AckWait:     2 * time.Hour,
		MaxAttempts: 5,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	profile := yacycrawlcontract.NewCrawlProfile(yacycrawlcontract.CrawlProfile{
		Scope:           yacycrawlcontract.ScopeDomain,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
	})
	order := yacycrawlcontract.CrawlOrder{
		OrderID: uuid.NewString(),
		Profile: profile,
		Requests: []yacycrawlcontract.CrawlRequest{
			{URL: "https://example.com/", ProfileHandle: profile.Handle},
		},
	}
	naked := make(chan struct{})
	delivery := crawlorder.CrawlOrderDelivery{
		Order: order,
		Ack:   func(context.Context) error { t.Error("failed run must not ack"); return nil },
		Nak:   func(context.Context) error { close(naked); return nil },
	}
	if err := queue.Publish(ctx, delivery); err != nil {
		t.Fatalf("publish delivery: %v", err)
	}

	select {
	case job := <-f.Jobs():
		f.Done(job, true)
	case <-time.After(3 * time.Second):
		t.Fatal("frontier never received seeded job")
	}

	select {
	case <-naked:
	case <-time.After(3 * time.Second):
		t.Fatal("delivery never naked after a page failed to deliver")
	}
}

func TestConsumerTermsMalformedOrderID(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](4)
	f := frontier.NewFrontier(8, nil)
	consumer := crawlorder.NewCrawlOrderConsumer(queue, f, crawlorder.OrderRedeliveryPolicy{
		AckWait:     2 * time.Hour,
		MaxAttempts: 5,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	order := yacycrawlcontract.CrawlOrder{OrderID: "not-a-uuid"}
	termed := make(chan struct{})
	delivery := crawlorder.CrawlOrderDelivery{
		Order: order,
		Term:  func(context.Context) error { close(termed); return nil },
		Ack: func(context.Context) error {
			t.Error("malformed order id must not ack")
			return nil
		},
	}
	if err := queue.Publish(ctx, delivery); err != nil {
		t.Fatalf("publish delivery: %v", err)
	}

	select {
	case <-termed:
	case <-time.After(3 * time.Second):
		t.Fatal("malformed order id was not termed")
	}
}

func TestConsumerNaksDuplicateOrderID(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](4)
	f := frontier.NewFrontier(8, nil)
	consumer := crawlorder.NewCrawlOrderConsumer(queue, f, crawlorder.OrderRedeliveryPolicy{
		AckWait:     2 * time.Hour,
		MaxAttempts: 5,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	profile := yacycrawlcontract.NewCrawlProfile(yacycrawlcontract.CrawlProfile{
		Scope:           yacycrawlcontract.ScopeDomain,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
	})
	orderID := uuid.NewString()
	newDelivery := func(ack, nak chan struct{}) crawlorder.CrawlOrderDelivery {
		return crawlorder.CrawlOrderDelivery{
			Order: yacycrawlcontract.CrawlOrder{
				OrderID: orderID,
				Profile: profile,
				Requests: []yacycrawlcontract.CrawlRequest{
					{URL: "https://example.com/", ProfileHandle: profile.Handle},
				},
			},
			Ack: func(context.Context) error {
				if ack != nil {
					close(ack)
				}
				return nil
			},
			Nak: func(context.Context) error {
				if nak != nil {
					close(nak)
				}
				return nil
			},
		}
	}

	firstAck := make(chan struct{})
	if err := queue.Publish(ctx, newDelivery(firstAck, nil)); err != nil {
		t.Fatalf("publish first: %v", err)
	}

	var job crawljob.CrawlJob
	select {
	case job = <-f.Jobs():
	case <-time.After(3 * time.Second):
		t.Fatal("frontier never received seeded job")
	}

	duplicateNak := make(chan struct{})
	if err := queue.Publish(ctx, newDelivery(nil, duplicateNak)); err != nil {
		t.Fatalf("publish duplicate: %v", err)
	}

	select {
	case <-duplicateNak:
	case <-time.After(3 * time.Second):
		t.Fatal("duplicate order id was not naked")
	}

	f.Done(job, false)
	select {
	case <-firstAck:
	case <-time.After(3 * time.Second):
		t.Fatal("first delivery never acked after its run finished")
	}
}

func TestConsumerTermsUncompilableProfile(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](4)
	f := frontier.NewFrontier(8, nil)
	consumer := crawlorder.NewCrawlOrderConsumer(queue, f, crawlorder.OrderRedeliveryPolicy{
		AckWait:     2 * time.Hour,
		MaxAttempts: 5,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	order := yacycrawlcontract.CrawlOrder{
		OrderID: uuid.NewString(),
		Profile: yacycrawlcontract.CrawlProfile{URLMustMatch: "("},
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
