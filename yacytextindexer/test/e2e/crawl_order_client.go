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
	ordersSubject        = "yacy.crawl.orders"
	ingestSubject        = "yacy.crawl.ingest"
	ingestMaxMsgs        = 1024
	extractedTextSubject = "yacy.crawl.extracted-text"
	extractedTextMaxMsgs = 1024
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
	spec := yacycrawlcontract.StreamSpec{
		OrdersSubject: ordersSubject,
		IngestSubject: ingestSubject,
		IngestMaxMsgs: ingestMaxMsgs,
	}
	if err := yacycrawlcontract.EnsureStreams(ctx, js, spec); err != nil {
		t.Fatalf("ensure streams: %v", err)
	}
	textSpec := yacycrawlcontract.ExtractedTextStreamSpec{
		Subject: extractedTextSubject,
		MaxMsgs: extractedTextMaxMsgs,
	}
	if err := yacycrawlcontract.EnsureExtractedTextStream(ctx, js, textSpec); err != nil {
		t.Fatalf("ensure extracted text stream: %v", err)
	}
}

func publishCrawlOrder(t *testing.T, ctx context.Context, js jetstream.JetStream, originURL string) {
	t.Helper()
	order := yacycrawlcontract.CrawlOrder{
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
