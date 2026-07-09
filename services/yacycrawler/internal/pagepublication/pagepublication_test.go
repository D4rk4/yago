package pagepublication_test

import (
	"context"
	"errors"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pagepublication"
)

func startJetStream(t *testing.T) jetstream.JetStream {
	t.Helper()
	srv, err := natsserver.NewServer(&natsserver.Options{
		Port: -1, JetStream: true, StoreDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats not ready")
	}
	t.Cleanup(srv.Shutdown)
	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	return js
}

func samplePage() crawlcapability.ExtractedPage {
	return crawlcapability.ExtractedPage{
		CanonicalURL: "http://example.com/a", Title: "Hi", Text: "the quick brown fox",
		Language: "en", FetchedAt: time.Unix(1_700_000_000, 0), LocalLinkCount: 1,
	}
}

func TestIndexOutputPublishes(t *testing.T) {
	js := startJetStream(t)
	ctx := context.Background()
	if err := yacycrawlcontract.EnsureCrawledPageIndexStream(ctx, js,
		yacycrawlcontract.CrawledPageIndexStreamSpec{Subject: "yacy.crawl.page-index", MaxMsgs: 10},
	); err != nil {
		t.Fatal(err)
	}
	output := pagepublication.NewIndexOutput(js, "yacy.crawl.page-index")
	if output.Name() != "index" {
		t.Fatalf("name = %q", output.Name())
	}
	if err := output.Publish(ctx, samplePage()); err != nil {
		t.Fatalf("publish: %v", err)
	}

	msg := consumeOne(t, js, yacycrawlcontract.CrawledPageIndexStreamName)
	message, err := yacycrawlcontract.UnmarshalCrawledPageIndexMessage(msg)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if message.CanonicalURL != "http://example.com/a" {
		t.Fatalf("canonical url = %q", message.CanonicalURL)
	}
	if len(message.Metadata) != 1 || len(message.Postings) != 0 {
		t.Fatalf("first message = %+v, want metadata only", message)
	}
}

func TestPageContentOutputPublishes(t *testing.T) {
	js := startJetStream(t)
	ctx := context.Background()
	if err := yacycrawlcontract.EnsureCrawledPageStream(ctx, js,
		yacycrawlcontract.CrawledPageStreamSpec{Subject: "yacy.crawl.pages", MaxMsgs: 10},
	); err != nil {
		t.Fatal(err)
	}
	output := pagepublication.NewPageContentOutput(js, "yacy.crawl.pages")
	if output.Name() != "page-content" {
		t.Fatalf("name = %q", output.Name())
	}
	if err := output.Publish(ctx, samplePage()); err != nil {
		t.Fatalf("publish: %v", err)
	}

	msg := consumeOne(t, js, yacycrawlcontract.CrawledPageStreamName)
	page, err := yacycrawlcontract.UnmarshalCrawledPage(msg)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if page.Title != "Hi" {
		t.Fatalf("title = %q", page.Title)
	}
}

func TestPublishFullStreamIsRetryable(t *testing.T) {
	js := startJetStream(t)
	ctx := context.Background()
	if err := yacycrawlcontract.EnsureCrawledPageStream(ctx, js,
		yacycrawlcontract.CrawledPageStreamSpec{Subject: "yacy.crawl.pages", MaxMsgs: 1},
	); err != nil {
		t.Fatal(err)
	}
	output := pagepublication.NewPageContentOutput(js, "yacy.crawl.pages")
	if err := output.Publish(ctx, samplePage()); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	err := output.Publish(ctx, samplePage())
	var retryable crawlcapability.TransientPublicationError
	if err == nil || !errors.As(err, &retryable) {
		t.Fatalf("full stream should yield TransientPublicationError, got %v", err)
	}
}

func consumeOne(t *testing.T, js jetstream.JetStream, stream string) []byte {
	t.Helper()
	consumer, err := js.CreateOrUpdateConsumer(context.Background(), stream,
		jetstream.ConsumerConfig{AckPolicy: jetstream.AckExplicitPolicy})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := consumer.Next(jetstream.FetchMaxWait(5 * time.Second))
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	_ = msg.Ack()
	return msg.Data()
}
