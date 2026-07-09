//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

const (
	ordersSubject           = "yacy.crawl.orders"
	crawledPageIndexSubject = "yacy.crawl.page-index"
	crawledPageIndexMaxMsgs = 1024
)

func connectJetStream(t *testing.T, url string) jetstream.JetStream {
	t.Helper()
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect nats: %v", err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("init jetstream: %v", err)
	}
	return js
}

func ensureStreams(t *testing.T, ctx context.Context, js jetstream.JetStream) {
	t.Helper()
	if err := yacycrawlcontract.EnsureOrdersStream(ctx, js, yacycrawlcontract.OrdersStreamSpec{
		Subject: ordersSubject,
	}); err != nil {
		t.Fatalf("ensure orders stream: %v", err)
	}
	if err := yacycrawlcontract.EnsureCrawledPageIndexStream(
		ctx,
		js,
		yacycrawlcontract.CrawledPageIndexStreamSpec{
			Subject: crawledPageIndexSubject,
			MaxMsgs: crawledPageIndexMaxMsgs,
		},
	); err != nil {
		t.Fatalf("ensure crawled page index stream: %v", err)
	}
}

func fetchOneCrawledPageIndex(
	t *testing.T,
	ctx context.Context,
	js jetstream.JetStream,
) yacycrawlcontract.CrawledPageIndex {
	t.Helper()
	stream, err := js.Stream(ctx, yacycrawlcontract.CrawledPageIndexStreamName)
	if err != nil {
		t.Fatalf("lookup crawled page index stream: %v", err)
	}
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		AckPolicy: jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatalf("create crawled page index consumer: %v", err)
	}

	var index yacycrawlcontract.CrawledPageIndex
	for len(index.Postings) == 0 {
		msg, err := consumer.Next(jetstream.FetchMaxWait(60 * time.Second))
		if err != nil {
			t.Fatalf("fetch crawled page index message: %v", err)
		}
		message, err := yacycrawlcontract.UnmarshalCrawledPageIndexMessage(msg.Data())
		if err != nil {
			t.Fatalf("decode crawled page index message: %v", err)
		}
		if index.CanonicalURL == "" {
			index.CanonicalURL = message.CanonicalURL
		}
		index.Metadata = append(index.Metadata, message.Metadata...)
		index.Postings = append(index.Postings, message.Postings...)
		if err := msg.Ack(); err != nil {
			t.Fatalf("ack: %v", err)
		}
	}
	return index
}
