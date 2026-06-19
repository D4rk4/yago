package yacycrawler_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/yacycrawler"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const (
	testOrdersSubject = "yacy.crawl.test.orders"
	testIngestSubject = "yacy.crawl.test.ingest"
)

func testIngestBatch(url string) yacycrawler.IngestBatch {
	return yacycrawler.IngestBatch{
		SourceURL:     url,
		Provenance:    []byte("admin"),
		ProfileHandle: "abcdef012345",
		Postings: []yacymodel.RWIEntry{
			{
				WordHash:   yacymodel.Hash("wordhash0123"),
				Properties: map[string]string{"u": "urlhash01234"},
			},
		},
	}
}

func TestNATSIngestPublisherDelivers(t *testing.T) {
	js := connectJetStream(t, startNATS(t))
	ctx := context.Background()
	if err := yacycrawler.EnsureStreams(ctx, js, yacycrawler.StreamSpec{
		OrdersSubject: testOrdersSubject,
		IngestSubject: testIngestSubject,
		IngestMaxMsgs: 64,
	}); err != nil {
		t.Fatalf("ensure streams: %v", err)
	}

	publisher := yacycrawler.NewNATSIngestPublisher(js, testIngestSubject)
	const count = 5
	want := make([]yacycrawler.IngestBatch, 0, count)
	for i := range count {
		batch := testIngestBatch("https://example.org/" + string(rune('a'+i)))
		want = append(want, batch)
		if err := publisher.Publish(ctx, batch); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}

	got := drainIngest(t, js, count)
	if !reflect.DeepEqual(want, got) {
		t.Errorf("delivered batches mismatch:\nwant %#v\ngot  %#v", want, got)
	}
}

func TestNATSIngestPublisherBackpressureBlocksThenDrains(t *testing.T) {
	js := connectJetStream(t, startNATS(t))
	ctx := context.Background()
	if err := yacycrawler.EnsureStreams(ctx, js, yacycrawler.StreamSpec{
		OrdersSubject: testOrdersSubject,
		IngestSubject: testIngestSubject,
		IngestMaxMsgs: 1,
	}); err != nil {
		t.Fatalf("ensure streams: %v", err)
	}
	publisher := yacycrawler.NewNATSIngestPublisher(js, testIngestSubject)

	if err := publisher.Publish(ctx, testIngestBatch("https://example.org/first")); err != nil {
		t.Fatalf("first publish: %v", err)
	}

	blocked := make(chan error, 1)
	go func() {
		blocked <- publisher.Publish(ctx, testIngestBatch("https://example.org/second"))
	}()

	select {
	case err := <-blocked:
		t.Fatalf("second publish returned %v before stream drained, want blocking", err)
	case <-time.After(300 * time.Millisecond):
	}

	drainIngest(t, js, 1)

	select {
	case err := <-blocked:
		if err != nil {
			t.Fatalf("second publish after drain: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second publish did not unblock after drain")
	}
}

func TestNATSIngestPublisherBackpressureRespectsDeadline(t *testing.T) {
	js := connectJetStream(t, startNATS(t))
	if err := yacycrawler.EnsureStreams(context.Background(), js, yacycrawler.StreamSpec{
		OrdersSubject: testOrdersSubject,
		IngestSubject: testIngestSubject,
		IngestMaxMsgs: 1,
	}); err != nil {
		t.Fatalf("ensure streams: %v", err)
	}
	publisher := yacycrawler.NewNATSIngestPublisher(js, testIngestSubject)
	if err := publisher.Publish(
		context.Background(),
		testIngestBatch("https://example.org/first"),
	); err != nil {
		t.Fatalf("first publish: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := publisher.Publish(ctx, testIngestBatch("https://example.org/second")); err == nil {
		t.Fatal("expected deadline error on saturated ingest stream, got nil")
	}
}

func drainIngest(t *testing.T, js jetstream.JetStream, count int) []yacycrawler.IngestBatch {
	t.Helper()
	stream, err := js.Stream(context.Background(), yacycrawler.IngestStreamName)
	if err != nil {
		t.Fatalf("lookup ingest stream: %v", err)
	}
	consumer, err := stream.CreateOrUpdateConsumer(context.Background(), jetstream.ConsumerConfig{
		AckPolicy: jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatalf("create drain consumer: %v", err)
	}
	out := make([]yacycrawler.IngestBatch, 0, count)
	for len(out) < count {
		msgs, err := consumer.Fetch(count-len(out), jetstream.FetchMaxWait(2*time.Second))
		if err != nil {
			t.Fatalf("fetch ingest: %v", err)
		}
		for msg := range msgs.Messages() {
			batch, err := yacycrawlcontract.UnmarshalIngestBatch(msg.Data())
			if err != nil {
				t.Fatalf("decode ingest: %v", err)
			}
			out = append(out, batch)
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
