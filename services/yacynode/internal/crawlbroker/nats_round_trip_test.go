package crawlbroker_test

import (
	"context"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/crawlbroker"
)

const (
	ordersSubject = "yacy.crawl.orders"
	ingestSubject = "yacy.crawl.ingest"
)

func openBroker(t *testing.T) (*crawlbroker.CrawlBroker, jetstream.JetStream, context.Context) {
	t.Helper()
	url := startNATS(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	broker, err := crawlbroker.Open(ctx, crawlbroker.Config{
		NATSURL:       url,
		OrdersSubject: ordersSubject,
		IngestSubject: ingestSubject,
		IngestDurable: "yacy-node",
		IngestMaxMsgs: 16,
	})
	if err != nil {
		t.Fatalf("open broker: %v", err)
	}
	t.Cleanup(broker.Close)
	return broker, connectJetStream(t, url), ctx
}

func TestOrderPublisherDeliversToOrdersStream(t *testing.T) {
	broker, js, ctx := openBroker(t)

	order := yacycrawlcontract.CrawlOrder{
		OrderID:  "order-1",
		Profile:  yacycrawlcontract.NewCrawlProfile(yacycrawlcontract.CrawlProfile{Name: "docs"}),
		SeedURLs: []string{"https://example.org"},
	}
	if err := broker.Orders.Publish(ctx, order); err != nil {
		t.Fatalf("publish order: %v", err)
	}

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

func TestIngestReceiverDeliversDecodableBatchAndSkipsGarbage(t *testing.T) {
	broker, js, ctx := openBroker(t)

	if _, err := js.Publish(ctx, ingestSubject, []byte("not json")); err != nil {
		t.Fatalf("publish garbage: %v", err)
	}
	message := yacycrawlcontract.CrawledPageIndexMessage{
		CanonicalURL: "https://example.org",
	}
	data, err := yacycrawlcontract.MarshalCrawledPageIndexMessage(message)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}
	if _, err := js.Publish(ctx, ingestSubject, data); err != nil {
		t.Fatalf("publish message: %v", err)
	}

	select {
	case delivery := <-broker.Ingest.Receive():
		if delivery.Message.CanonicalURL != message.CanonicalURL {
			t.Fatalf(
				"canonicalURL = %q, want %q",
				delivery.Message.CanonicalURL,
				message.CanonicalURL,
			)
		}
		if err := delivery.Ack(ctx); err != nil {
			t.Fatalf("ack: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no ingest delivery received")
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
