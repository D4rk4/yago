//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/D4rk4/yago/yacycrawlcontract"
)

const (
	ordersSubject = "yacy.crawl.orders"
	ingestSubject = "yacy.crawl.ingest"
	ingestMaxMsgs = 1024
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
}

func fetchOneIngest(
	t *testing.T,
	ctx context.Context,
	js jetstream.JetStream,
) yacycrawlcontract.IngestBatch {
	t.Helper()
	stream, err := js.Stream(ctx, yacycrawlcontract.IngestStreamName)
	if err != nil {
		t.Fatalf("lookup ingest stream: %v", err)
	}
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		AckPolicy: jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatalf("create ingest consumer: %v", err)
	}
	msgs, err := consumer.Fetch(1, jetstream.FetchMaxWait(60*time.Second))
	if err != nil {
		t.Fatalf("fetch ingest: %v", err)
	}
	msg, ok := <-msgs.Messages()
	if !ok {
		if err := msgs.Error(); err != nil {
			t.Fatalf("fetch error: %v", err)
		}
		t.Fatal("no ingest batch received")
	}
	batch, err := yacycrawlcontract.UnmarshalIngestBatch(msg.Data())
	if err != nil {
		t.Fatalf("decode ingest: %v", err)
	}
	if err := msg.Ack(); err != nil {
		t.Fatalf("ack: %v", err)
	}
	return batch
}
