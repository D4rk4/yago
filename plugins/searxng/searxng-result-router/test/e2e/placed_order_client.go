//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

const (
	ordersSubject      = "yacy.crawl.orders"
	orderPlacementWait = 30 * time.Second
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

func ensureOrdersStream(t *testing.T, ctx context.Context, js jetstream.JetStream) {
	t.Helper()
	if err := yacycrawlcontract.EnsureOrdersStream(ctx, js, yacycrawlcontract.OrdersStreamSpec{
		Subject: ordersSubject,
	}); err != nil {
		t.Fatalf("ensure orders stream: %v", err)
	}
}

func fetchOnePlacedOrder(
	t *testing.T,
	ctx context.Context,
	js jetstream.JetStream,
) yacycrawlcontract.CrawlOrder {
	t.Helper()
	consumer, err := js.CreateOrUpdateConsumer(
		ctx,
		yacycrawlcontract.OrdersStreamName,
		jetstream.ConsumerConfig{
			FilterSubject: ordersSubject,
			AckPolicy:     jetstream.AckExplicitPolicy,
		},
	)
	if err != nil {
		t.Fatalf("create orders consumer: %v", err)
	}

	msgs, err := consumer.Fetch(1, jetstream.FetchMaxWait(orderPlacementWait))
	if err != nil {
		t.Fatalf("fetch placed order: %v", err)
	}

	var order yacycrawlcontract.CrawlOrder
	for msg := range msgs.Messages() {
		order, err = yacycrawlcontract.UnmarshalCrawlOrder(msg.Data())
		if err != nil {
			t.Fatalf("unmarshal placed order: %v", err)
		}
		_ = msg.Ack()
	}
	if err := msgs.Error(); err != nil {
		t.Fatalf("consume placed order: %v", err)
	}
	if order.OrderID == "" {
		t.Fatal("no crawl order was placed on " + ordersSubject)
	}
	return order
}
