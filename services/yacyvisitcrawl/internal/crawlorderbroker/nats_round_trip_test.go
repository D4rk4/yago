package crawlorderbroker_test

import (
	"context"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacyvisitcrawl/internal/crawlorderbroker"
)

const ordersSubject = "yacy.crawl.orders"

func TestOrderPlacementDeliversToOrdersStream(t *testing.T) {
	url := startNATS(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	broker, err := crawlorderbroker.Open(ctx, crawlorderbroker.Config{
		NATSURL:       url,
		OrdersSubject: ordersSubject,
	})
	if err != nil {
		t.Fatalf("open broker: %v", err)
	}
	t.Cleanup(broker.Close)

	order := yacycrawlcontract.CrawlOrder{
		OrderID:  "order-1",
		Profile:  yacycrawlcontract.NewCrawlProfile(yacycrawlcontract.CrawlProfile{Name: "docs"}),
		SeedURLs: []string{"https://example.org"},
	}
	if err := broker.Orders.Place(ctx, order); err != nil {
		t.Fatalf("place order: %v", err)
	}

	js := connectJetStream(t, url)
	consumer, err := js.CreateOrUpdateConsumer(
		ctx,
		yacycrawlcontract.OrdersStreamName,
		jetstream.ConsumerConfig{
			AckPolicy:     jetstream.AckExplicitPolicy,
			FilterSubject: ordersSubject,
		},
	)
	if err != nil {
		t.Fatalf("orders consumer: %v", err)
	}
	msg, err := consumer.Next(jetstream.FetchMaxWait(5 * time.Second))
	if err != nil {
		t.Fatalf("fetch order: %v", err)
	}
	got, err := yacycrawlcontract.UnmarshalCrawlOrder(msg.Data())
	if err != nil {
		t.Fatalf("decode order: %v", err)
	}
	if got.OrderID != order.OrderID || got.Profile.Handle != order.Profile.Handle {
		t.Fatalf("round-tripped order mismatch: %+v", got)
	}
}

func TestOpenRejectsUnreachableNATS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := crawlorderbroker.Open(ctx, crawlorderbroker.Config{
		NATSURL:       "nats://127.0.0.1:1",
		OrdersSubject: ordersSubject,
	}); err == nil {
		t.Fatal("unreachable nats should fail to open")
	}
}

func startNATS(t *testing.T) string {
	t.Helper()
	srv, err := natsserver.NewServer(&natsserver.Options{
		Port:      -1,
		JetStream: true,
		StoreDir:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("new nats server: %v", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	t.Cleanup(srv.Shutdown)
	return srv.ClientURL()
}

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
