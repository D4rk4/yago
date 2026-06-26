package crawlorder_test

import (
	"context"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/boundedqueue"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlorder"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlwork"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/frontier"
)

func TestConsumerSeedsFrontierAndAcks(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[crawlwork.CrawlOrderDelivery](4)
	f := frontier.NewFrontier(8)
	consumer := crawlorder.NewCrawlOrderConsumer(queue, f)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	profile := yacycrawlcontract.NewCrawlProfile(yacycrawlcontract.CrawlProfile{
		Scope:           yacycrawlcontract.ScopeDomain,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
	})
	order := yacycrawlcontract.CrawlOrder{
		Provenance: []byte("admin"),
		Profile:    profile,
		Requests: []yacycrawlcontract.CrawlRequest{
			{URL: "https://example.com/", ProfileHandle: profile.Handle},
		},
	}
	acked := make(chan struct{})
	delivery := crawlwork.NewCrawlOrderDelivery(order)
	delivery.Ack = func(context.Context) error { close(acked); return nil }
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
		f.Done(job)
	case <-time.After(3 * time.Second):
		t.Fatal("frontier never received seeded job")
	}

	select {
	case <-acked:
	case <-time.After(3 * time.Second):
		t.Fatal("delivery never acked after run finished")
	}
}

func TestConsumerTermsUncompilableProfile(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[crawlwork.CrawlOrderDelivery](4)
	f := frontier.NewFrontier(8)
	consumer := crawlorder.NewCrawlOrderConsumer(queue, f)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	order := yacycrawlcontract.CrawlOrder{
		Profile: yacycrawlcontract.CrawlProfile{URLMustMatch: "("},
	}
	termed := make(chan struct{})
	delivery := crawlwork.NewCrawlOrderDelivery(order)
	delivery.Term = func(context.Context) error { close(termed); return nil }
	delivery.Ack = func(context.Context) error {
		t.Error("uncompilable profile must not ack")
		return nil
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
