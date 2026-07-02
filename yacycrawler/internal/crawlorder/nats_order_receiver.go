package crawlorder

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/D4rk4/yago/yacycrawlcontract"
)

const msgOrderDecodeFailed = "crawl order decode failed"

var consumeCrawlOrderMessages = func(
	consumer jetstream.Consumer,
	handler jetstream.MessageHandler,
) (jetstream.ConsumeContext, error) {
	return consumer.Consume(handler)
}

type NATSOrderReceiver struct {
	out chan CrawlOrderDelivery
}

func NewNATSOrderReceiver(
	ctx context.Context,
	js jetstream.JetStream,
	durable string,
	subject string,
) (*NATSOrderReceiver, error) {
	consumer, err := js.CreateOrUpdateConsumer(
		ctx,
		yacycrawlcontract.OrdersStreamName,
		jetstream.ConsumerConfig{
			Durable:       durable,
			AckPolicy:     jetstream.AckExplicitPolicy,
			FilterSubject: subject,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("create orders consumer: %w", err)
	}

	out := make(chan CrawlOrderDelivery)
	consume, err := consumeCrawlOrderMessages(consumer, func(msg jetstream.Msg) {
		deliverCrawlOrder(ctx, out, msg)
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

func (r *NATSOrderReceiver) Receive() <-chan CrawlOrderDelivery {
	return r.out
}

func deliverCrawlOrder(ctx context.Context, out chan<- CrawlOrderDelivery, msg jetstream.Msg) {
	order, err := yacycrawlcontract.UnmarshalCrawlOrder(msg.Data())
	if err != nil {
		slog.WarnContext(context.Background(), msgOrderDecodeFailed, slog.Any("error", err))
		_ = msg.Term()
		return
	}
	delivery := CrawlOrderDelivery{
		Order: order,
		Ack: func(context.Context) error {
			return msg.Ack()
		},
		Nak: func(context.Context) error {
			return msg.Nak()
		},
		Term: func(context.Context) error {
			return msg.Term()
		},
	}
	select {
	case out <- delivery:
	case <-ctx.Done():
		_ = msg.Nak()
	}
}
