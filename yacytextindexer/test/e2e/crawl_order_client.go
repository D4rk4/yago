//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

const (
	ordersSubject           = "yacy.crawl.orders"
	crawledPageIndexSubject = "yacy.crawl.page-index"
	crawledPageIndexMaxMsgs = 1024
	crawledPageSubject      = "yacy.crawl.pages"
	crawledPageMaxMsgs      = 1024
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
	pageSpec := yacycrawlcontract.CrawledPageStreamSpec{
		Subject: crawledPageSubject,
		MaxMsgs: crawledPageMaxMsgs,
	}
	if err := yacycrawlcontract.EnsureCrawledPageStream(ctx, js, pageSpec); err != nil {
		t.Fatalf("ensure crawled page stream: %v", err)
	}
}

func publishCrawlOrder(
	t *testing.T,
	ctx context.Context,
	js jetstream.JetStream,
	originURL string,
) {
	t.Helper()
	order := yacycrawlcontract.CrawlOrder{
		OrderID:    "b3f2a1c0-4d5e-4f6a-8b9c-0d1e2f3a4b5c",
		Provenance: []byte("admin"),
		Profile: yacycrawlcontract.NewCrawlProfile(yacycrawlcontract.CrawlProfile{
			Name:            "default",
			Scope:           yacycrawlcontract.ScopeDomain,
			URLMustMatch:    yacycrawlcontract.MatchAll,
			MaxDepth:        0,
			MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
		}),
	}
	order.Requests = []yacycrawlcontract.CrawlRequest{
		{URL: originURL, ProfileHandle: order.Profile.Handle},
	}
	data, err := yacycrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal order: %v", err)
	}
	if _, err := js.Publish(ctx, ordersSubject, data); err != nil {
		t.Fatalf("publish order: %v", err)
	}
}
