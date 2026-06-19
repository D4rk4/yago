package yacycrawler

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

const msgOrderDecodeFailed = "crawl order decode failed"

type NATSOrderReceiver struct {
	out chan yacycrawlcontract.CrawlOrder
}

func NewNATSOrderReceiver(
	ctx context.Context,
	js jetstream.JetStream,
	durable string,
	subject string,
) (*NATSOrderReceiver, error) {
	consumer, err := js.CreateOrUpdateConsumer(ctx, OrdersStreamName, jetstream.ConsumerConfig{
		Durable:       durable,
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: subject,
	})
	if err != nil {
		return nil, fmt.Errorf("create orders consumer: %w", err)
	}

	out := make(chan yacycrawlcontract.CrawlOrder)
	consume, err := consumer.Consume(func(msg jetstream.Msg) {
		order, err := yacycrawlcontract.UnmarshalCrawlOrder(msg.Data())
		if err != nil {
			slog.Warn(msgOrderDecodeFailed, "error", err)
			_ = msg.Term()
			return
		}
		select {
		case out <- order:
			_ = msg.Ack()
		case <-ctx.Done():
			_ = msg.Nak()
		}
	})
	if err != nil {
		return nil, fmt.Errorf("consume orders: %w", err)
	}

	go func() {
		<-ctx.Done()
		consume.Stop()
	}()

	return &NATSOrderReceiver{out: out}, nil
}

func (r *NATSOrderReceiver) Receive() <-chan yacycrawlcontract.CrawlOrder {
	return r.out
}
