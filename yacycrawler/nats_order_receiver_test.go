package yacycrawler_test

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler"
)

func ensureTestStreams(t *testing.T, js jetstream.JetStream) {
	t.Helper()
	if err := yacycrawler.EnsureStreams(context.Background(), js, yacycrawler.StreamSpec{
		OrdersSubject: testOrdersSubject,
		IngestSubject: testIngestSubject,
		IngestMaxMsgs: 64,
	}); err != nil {
		t.Fatalf("ensure streams: %v", err)
	}
}

func TestNATSOrderReceiverDeliversInOrder(t *testing.T) {
	js := connectJetStream(t, startNATS(t))
	ensureTestStreams(t, js)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	receiver, err := yacycrawler.NewNATSOrderReceiver(ctx, js, "test-durable", testOrdersSubject)
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
			if len(got.Provenance) != 1 || got.Provenance[0] != byte(i) {
				t.Errorf("order %d provenance = %v", i, got.Provenance)
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
	receiver, err := yacycrawler.NewNATSOrderReceiver(ctx, js, "test-durable", testOrdersSubject)
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
		if string(got.Provenance) != "good" {
			t.Errorf("provenance = %q, want good", got.Provenance)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("poison message stalled the receive loop")
	}
}
