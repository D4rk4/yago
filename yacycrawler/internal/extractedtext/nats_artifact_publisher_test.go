package extractedtext_test

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/extractedtext"
)

const testExtractedTextSubject = "yacy.crawl.test.extracted-text"

func testExtractedText(url string) yacycrawlcontract.ExtractedText {
	return yacycrawlcontract.ExtractedText{
		CanonicalURL: url,
		DocumentID:   "abc123",
		Title:        "Hi",
		Text:         "words here",
		CrawledAt:    time.Unix(0, 0).UTC(),
		Language:     "en",
	}
}

func TestNATSArtifactPublisherDelivers(t *testing.T) {
	js := connectJetStream(t, startNATS(t))
	ctx := context.Background()
	if err := yacycrawlcontract.EnsureExtractedTextStream(ctx, js, yacycrawlcontract.ExtractedTextStreamSpec{
		Subject: testExtractedTextSubject,
		MaxMsgs: 64,
	}); err != nil {
		t.Fatalf("ensure extracted text stream: %v", err)
	}

	publisher := extractedtext.NewNATSArtifactPublisher(js, testExtractedTextSubject)
	text := testExtractedText("https://example.org/a")
	if err := publisher.Publish(ctx, text); err != nil {
		t.Fatalf("publish: %v", err)
	}

	got := drainExtractedText(t, js, 1)
	if len(got) != 1 || got[0].CanonicalURL != text.CanonicalURL {
		t.Errorf("delivered = %#v, want %#v", got, text)
	}
}

func TestNATSArtifactPublisherBackpressureBlocksThenDrains(t *testing.T) {
	js := connectJetStream(t, startNATS(t))
	ctx := context.Background()
	if err := yacycrawlcontract.EnsureExtractedTextStream(ctx, js, yacycrawlcontract.ExtractedTextStreamSpec{
		Subject: testExtractedTextSubject,
		MaxMsgs: 1,
	}); err != nil {
		t.Fatalf("ensure extracted text stream: %v", err)
	}
	publisher := extractedtext.NewNATSArtifactPublisher(js, testExtractedTextSubject)

	if err := publisher.Publish(ctx, testExtractedText("https://example.org/first")); err != nil {
		t.Fatalf("first publish: %v", err)
	}

	blocked := make(chan error, 1)
	go func() {
		blocked <- publisher.Publish(ctx, testExtractedText("https://example.org/second"))
	}()

	select {
	case err := <-blocked:
		t.Fatalf("second publish returned %v before stream drained, want blocking", err)
	case <-time.After(300 * time.Millisecond):
	}

	drainExtractedText(t, js, 1)

	select {
	case err := <-blocked:
		if err != nil {
			t.Fatalf("second publish after drain: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second publish did not unblock after drain")
	}
}

func drainExtractedText(t *testing.T, js jetstream.JetStream, count int) []yacycrawlcontract.ExtractedText {
	t.Helper()
	stream, err := js.Stream(context.Background(), yacycrawlcontract.ExtractedTextStreamName)
	if err != nil {
		t.Fatalf("lookup extracted text stream: %v", err)
	}
	consumer, err := stream.CreateOrUpdateConsumer(context.Background(), jetstream.ConsumerConfig{
		AckPolicy: jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatalf("create drain consumer: %v", err)
	}
	out := make([]yacycrawlcontract.ExtractedText, 0, count)
	for len(out) < count {
		msgs, err := consumer.Fetch(count-len(out), jetstream.FetchMaxWait(2*time.Second))
		if err != nil {
			t.Fatalf("fetch extracted text: %v", err)
		}
		for msg := range msgs.Messages() {
			text, err := yacycrawlcontract.UnmarshalExtractedText(msg.Data())
			if err != nil {
				t.Fatalf("decode extracted text: %v", err)
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
