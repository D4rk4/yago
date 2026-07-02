package crawlorder_test

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacycrawler/internal/crawlorder"
)

const (
	testOrdersSubject = "yacy.crawl.test.orders"
	testIngestSubject = "yacy.crawl.test.ingest"
)

func ensureTestStreams(t *testing.T, js jetstream.JetStream) {
	t.Helper()
	if err := yacycrawlcontract.EnsureStreams(
		context.Background(),
		js,
		yacycrawlcontract.StreamSpec{
			OrdersSubject: testOrdersSubject,
			IngestSubject: testIngestSubject,
			IngestMaxMsgs: 64,
		},
	); err != nil {
		t.Fatalf("ensure streams: %v", err)
	}
}

func TestNATSOrderReceiverDeliversInOrder(t *testing.T) {
	js := connectJetStream(t, startNATS(t))
	ensureTestStreams(t, js)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	receiver, err := crawlorder.NewNATSOrderReceiver(ctx, js, "test-durable", testOrdersSubject)
	if err != nil {
		t.Fatalf("new receiver: %v", err)
	}

	const count = 4
	for i := range count {
		order := yacycrawlcontract.CrawlOrder{Provenance: []byte{byte(i)}}
		data, err := yacycrawlcontract.MarshalCrawlOrder(order)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if _, err := js.Publish(ctx, testOrdersSubject, data); err != nil {
			t.Fatalf("publish order %d: %v", i, err)
		}
	}

	for i := range count {
		select {
		case got := <-receiver.Receive():
			if len(got.Order.Provenance) != 1 || got.Order.Provenance[0] != byte(i) {
				t.Errorf("order %d provenance = %v", i, got.Order.Provenance)
			}
			if err := got.Ack(context.Background()); err != nil {
				t.Fatalf("ack order %d: %v", i, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for order %d", i)
		}
	}
}

func TestNATSOrderReceiverTermsPoisonWithoutStalling(t *testing.T) {
	js := connectJetStream(t, startNATS(t))
	ensureTestStreams(t, js)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	receiver, err := crawlorder.NewNATSOrderReceiver(ctx, js, "test-durable", testOrdersSubject)
	if err != nil {
		t.Fatalf("new receiver: %v", err)
	}

	if _, err := js.Publish(ctx, testOrdersSubject, []byte("not json")); err != nil {
		t.Fatalf("publish poison: %v", err)
	}
	good := yacycrawlcontract.CrawlOrder{Provenance: []byte("good")}
	data, err := yacycrawlcontract.MarshalCrawlOrder(good)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := js.Publish(ctx, testOrdersSubject, data); err != nil {
		t.Fatalf("publish good: %v", err)
	}

	select {
	case got := <-receiver.Receive():
		if string(got.Order.Provenance) != "good" {
			t.Errorf("provenance = %q, want good", got.Order.Provenance)
		}
		if err := got.Ack(context.Background()); err != nil {
			t.Fatalf("ack good: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("poison message stalled the receive loop")
	}
}

func TestNATSOrderReceiverLeavesDeliveredOrderPendingUntilAck(t *testing.T) {
	js := connectJetStream(t, startNATS(t))
	ensureTestStreams(t, js)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	receiver, err := crawlorder.NewNATSOrderReceiver(ctx, js, "test-durable", testOrdersSubject)
	if err != nil {
		t.Fatalf("new receiver: %v", err)
	}

	order := yacycrawlcontract.CrawlOrder{Provenance: []byte("pending")}
	data, err := yacycrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := js.Publish(ctx, testOrdersSubject, data); err != nil {
		t.Fatalf("publish order: %v", err)
	}

	var delivery crawlorder.CrawlOrderDelivery
	select {
	case delivery = <-receiver.Receive():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for order")
	}

	waitForAckPending(t, js, "test-durable", 1)
	if err := delivery.Ack(context.Background()); err != nil {
		t.Fatalf("ack: %v", err)
	}
	waitForAckPending(t, js, "test-durable", 0)
}

func TestNATSOrderReceiverReturnsCreateConsumerError(t *testing.T) {
	js := connectJetStream(t, startNATS(t))

	if _, err := crawlorder.NewNATSOrderReceiver(
		context.Background(),
		js,
		"missing-stream",
		testOrdersSubject,
	); err == nil {
		t.Fatal("expected error without orders stream")
	}
}

func waitForAckPending(
	t *testing.T,
	js jetstream.JetStream,
	durable string,
	want int,
) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		consumer, err := js.Consumer(
			context.Background(),
			yacycrawlcontract.OrdersStreamName,
			durable,
		)
		if err != nil {
			t.Fatalf("lookup consumer: %v", err)
		}
		info, err := consumer.Info(context.Background())
		if err != nil {
			t.Fatalf("consumer info: %v", err)
		}
		if info.NumAckPending == want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("ack pending did not become %d", want)
}
