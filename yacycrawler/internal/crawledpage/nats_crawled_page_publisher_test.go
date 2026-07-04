package crawledpage_test

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawledpage"
)

const testCrawledPageSubject = "yacy.crawl.test.pages"

func testCrawledPage(url string) yacycrawlcontract.CrawledPage {
	return yacycrawlcontract.CrawledPage{
		CanonicalURL: url,
		DocumentID:   "abc123",
		Title:        "Hi",
		Text:         "words here",
		CrawledAt:    time.Unix(0, 0).UTC(),
		Language:     "en",
	}
}

func TestNATSCrawledPagePublisherDelivers(t *testing.T) {
	js := connectJetStream(t, startNATS(t))
	ctx := context.Background()
	if err := yacycrawlcontract.EnsureCrawledPageStream(
		ctx,
		js,
		yacycrawlcontract.CrawledPageStreamSpec{
			Subject: testCrawledPageSubject,
			MaxMsgs: 64,
		},
	); err != nil {
		t.Fatalf("ensure crawled page stream: %v", err)
	}

	publisher := crawledpage.NewNATSCrawledPagePublisher(js, testCrawledPageSubject)
	text := testCrawledPage("https://example.org/a")
	if err := publisher.Publish(ctx, text); err != nil {
		t.Fatalf("publish: %v", err)
	}

	got := drainCrawledPage(t, js, 1)
	if len(got) != 1 || got[0].CanonicalURL != text.CanonicalURL {
		t.Errorf("delivered = %#v, want %#v", got, text)
	}
}

func TestNATSCrawledPagePublisherBackpressureBlocksThenDrains(t *testing.T) {
	js := connectJetStream(t, startNATS(t))
	ctx := context.Background()
	if err := yacycrawlcontract.EnsureCrawledPageStream(
		ctx,
		js,
		yacycrawlcontract.CrawledPageStreamSpec{
			Subject: testCrawledPageSubject,
			MaxMsgs: 1,
		},
	); err != nil {
		t.Fatalf("ensure crawled page stream: %v", err)
	}
	publisher := crawledpage.NewNATSCrawledPagePublisher(js, testCrawledPageSubject)

	if err := publisher.Publish(ctx, testCrawledPage("https://example.org/first")); err != nil {
		t.Fatalf("first publish: %v", err)
	}

	blocked := make(chan error, 1)
	go func() {
		blocked <- publisher.Publish(ctx, testCrawledPage("https://example.org/second"))
	}()

	select {
	case err := <-blocked:
		t.Fatalf("second publish returned %v before stream drained, want blocking", err)
	case <-time.After(300 * time.Millisecond):
	}

	drainCrawledPage(t, js, 1)

	select {
	case err := <-blocked:
		if err != nil {
			t.Fatalf("second publish after drain: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second publish did not unblock after drain")
	}
}

func drainCrawledPage(
	t *testing.T,
	js jetstream.JetStream,
	count int,
) []yacycrawlcontract.CrawledPage {
	t.Helper()
	stream, err := js.Stream(context.Background(), yacycrawlcontract.CrawledPageStreamName)
	if err != nil {
		t.Fatalf("lookup crawled page stream: %v", err)
	}
	consumer, err := stream.CreateOrUpdateConsumer(context.Background(), jetstream.ConsumerConfig{
		AckPolicy: jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatalf("create drain consumer: %v", err)
	}
	out := make([]yacycrawlcontract.CrawledPage, 0, count)
	for len(out) < count {
		msgs, err := consumer.Fetch(count-len(out), jetstream.FetchMaxWait(2*time.Second))
		if err != nil {
			t.Fatalf("fetch crawled page: %v", err)
		}
		for msg := range msgs.Messages() {
			text, err := yacycrawlcontract.UnmarshalCrawledPage(msg.Data())
			if err != nil {
				t.Fatalf("decode crawled page: %v", err)
			}
			out = append(out, text)
			if err := msg.Ack(); err != nil {
				t.Fatalf("ack: %v", err)
			}
		}
		if err := msgs.Error(); err != nil {
			t.Fatalf("fetch error: %v", err)
		}
	}
	return out
}
