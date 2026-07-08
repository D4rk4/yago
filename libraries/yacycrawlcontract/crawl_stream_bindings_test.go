package yacycrawlcontract_test

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

func TestEnsureOrdersStreamCreatesWorkQueue(t *testing.T) {
	js := connectJetStream(t, startNATS(t))

	if err := yacycrawlcontract.EnsureOrdersStream(
		context.Background(),
		js,
		yacycrawlcontract.OrdersStreamSpec{
			Subject: "yacy.crawl.orders",
		},
	); err != nil {
		t.Fatalf("ensure orders stream: %v", err)
	}

	orders, err := js.Stream(context.Background(), yacycrawlcontract.OrdersStreamName)
	if err != nil {
		t.Fatalf("orders stream: %v", err)
	}
	if got := orders.CachedInfo().Config.Retention; got != jetstream.WorkQueuePolicy {
		t.Fatalf("orders retention = %v, want WorkQueuePolicy", got)
	}
}

func TestEnsureCrawledPageIndexStreamCreatesBoundedStream(t *testing.T) {
	js := connectJetStream(t, startNATS(t))

	spec := yacycrawlcontract.CrawledPageIndexStreamSpec{
		Subject: "yacy.crawl.page-index",
		MaxMsgs: 8,
	}
	if err := yacycrawlcontract.EnsureCrawledPageIndexStream(
		context.Background(),
		js,
		spec,
	); err != nil {
		t.Fatalf("ensure crawled page index stream: %v", err)
	}

	pageIndex, err := js.Stream(context.Background(), yacycrawlcontract.CrawledPageIndexStreamName)
	if err != nil {
		t.Fatalf("crawled page index stream: %v", err)
	}
	cfg := pageIndex.CachedInfo().Config
	if cfg.MaxMsgs != spec.MaxMsgs {
		t.Fatalf("crawled page index MaxMsgs = %d, want %d", cfg.MaxMsgs, spec.MaxMsgs)
	}
	if cfg.Discard != jetstream.DiscardNew {
		t.Fatalf("crawled page index discard = %v, want DiscardNew", cfg.Discard)
	}
}

func TestEnsureCrawledPageStreamCreatesBoundedStream(t *testing.T) {
	js := connectJetStream(t, startNATS(t))

	spec := yacycrawlcontract.CrawledPageStreamSpec{Subject: "yacy.crawl.pages", MaxMsgs: 8}
	if err := yacycrawlcontract.EnsureCrawledPageStream(
		context.Background(),
		js,
		spec,
	); err != nil {
		t.Fatalf("ensure crawled page stream: %v", err)
	}

	stream, err := js.Stream(context.Background(), yacycrawlcontract.CrawledPageStreamName)
	if err != nil {
		t.Fatalf("crawled page stream: %v", err)
	}
	cfg := stream.CachedInfo().Config
	if cfg.MaxMsgs != spec.MaxMsgs {
		t.Fatalf("crawled page MaxMsgs = %d, want %d", cfg.MaxMsgs, spec.MaxMsgs)
	}
	if cfg.Discard != jetstream.DiscardNew {
		t.Fatalf("crawled page discard = %v, want DiscardNew", cfg.Discard)
	}
}

func TestEnsureStreamsAreIdempotent(t *testing.T) {
	js := connectJetStream(t, startNATS(t))
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if err := yacycrawlcontract.EnsureOrdersStream(
			ctx,
			js,
			yacycrawlcontract.OrdersStreamSpec{Subject: "yacy.crawl.orders"},
		); err != nil {
			t.Fatalf("ensure orders stream (pass %d): %v", i, err)
		}
		if err := yacycrawlcontract.EnsureCrawledPageIndexStream(
			ctx,
			js,
			yacycrawlcontract.CrawledPageIndexStreamSpec{
				Subject: "yacy.crawl.page-index",
				MaxMsgs: 8,
			},
		); err != nil {
			t.Fatalf("ensure crawled page index stream (pass %d): %v", i, err)
		}
		if err := yacycrawlcontract.EnsureCrawledPageStream(
			ctx,
			js,
			yacycrawlcontract.CrawledPageStreamSpec{Subject: "yacy.crawl.pages", MaxMsgs: 8},
		); err != nil {
			t.Fatalf("ensure crawled page stream (pass %d): %v", i, err)
		}
	}
}

func TestEnsureStreamsReportBrokerFailure(t *testing.T) {
	nc, err := nats.Connect(startNATS(t))
	if err != nil {
		t.Fatalf("connect nats: %v", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("init jetstream: %v", err)
	}
	nc.Close()

	ctx := context.Background()
	if err := yacycrawlcontract.EnsureOrdersStream(
		ctx,
		js,
		yacycrawlcontract.OrdersStreamSpec{Subject: "yacy.crawl.orders"},
	); err == nil {
		t.Error("ensure orders stream on closed connection should fail")
	}
	if err := yacycrawlcontract.EnsureCrawledPageIndexStream(
		ctx,
		js,
		yacycrawlcontract.CrawledPageIndexStreamSpec{Subject: "yacy.crawl.page-index", MaxMsgs: 8},
	); err == nil {
		t.Error("ensure crawled page index stream on closed connection should fail")
	}
	if err := yacycrawlcontract.EnsureCrawledPageStream(
		ctx,
		js,
		yacycrawlcontract.CrawledPageStreamSpec{Subject: "yacy.crawl.pages", MaxMsgs: 8},
	); err == nil {
		t.Error("ensure crawled page stream on closed connection should fail")
	}
}
