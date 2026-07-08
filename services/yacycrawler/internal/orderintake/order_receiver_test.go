package orderintake_test

import (
	"context"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/orderintake"
)

const ordersSubject = "yacy.crawl.orders"

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
	if err := yacycrawlcontract.EnsureOrdersStream(context.Background(), js,
		yacycrawlcontract.OrdersStreamSpec{Subject: ordersSubject}); err != nil {
		t.Fatal(err)
	}
	return js
}

func consumer(t *testing.T, js jetstream.JetStream) jetstream.Consumer {
	t.Helper()
	c, err := js.CreateOrUpdateConsumer(context.Background(), yacycrawlcontract.OrdersStreamName,
		jetstream.ConsumerConfig{Durable: "yacycrawler", AckPolicy: jetstream.AckExplicitPolicy})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestReceiverDeliversDecodedOrder(t *testing.T) {
	js := startJetStream(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	payload, err := yacycrawlcontract.MarshalCrawlOrder(yacycrawlcontract.CrawlOrder{
		OrderID: "o1", SeedURLs: []string{"http://a.com/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := js.Publish(ctx, ordersSubject, payload); err != nil {
		t.Fatal(err)
	}

	receiver, err := orderintake.NewOrderReceiver(ctx, consumer(t, js))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case delivery := <-receiver.Deliveries():
		if delivery.Order.OrderID != "o1" {
			t.Fatalf("order id = %q", delivery.Order.OrderID)
		}
		if err := delivery.ExtendOwnership(ctx); err != nil {
			t.Fatalf("extend ownership: %v", err)
		}
		if err := delivery.Ack(ctx); err != nil {
			t.Fatalf("ack: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no delivery received")
	}
}

func TestReceiverTermsUndecodableOrder(t *testing.T) {
	js := startJetStream(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := js.Publish(ctx, ordersSubject, []byte("not json")); err != nil {
		t.Fatal(err)
	}
	receiver, err := orderintake.NewOrderReceiver(ctx, consumer(t, js))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-receiver.Deliveries():
		t.Fatal("undecodable order should not be delivered")
	case <-time.After(500 * time.Millisecond):
	}
}
