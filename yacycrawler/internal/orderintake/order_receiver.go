package orderintake

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
)

const msgOrderDecodeFailed = "crawl order decode failed"

type OrderReceiver struct {
	deliveries chan crawlcapability.DeliveredOrder
}

func NewOrderReceiver(
	ctx context.Context,
	consumer jetstream.Consumer,
) (*OrderReceiver, error) {
	deliveries := make(chan crawlcapability.DeliveredOrder)
	consume, err := consumer.Consume(func(msg jetstream.Msg) {
		order, err := yacycrawlcontract.UnmarshalCrawlOrder(msg.Data())
		if err != nil {
			slog.WarnContext(ctx, msgOrderDecodeFailed, slog.Any("error", err))
			_ = msg.Term()
			return
		}
		delivery := crawlcapability.DeliveredOrder{
			Order:           order,
			Ack:             func(context.Context) error { return msg.Ack() },
			Retry:           func(context.Context) error { return msg.Nak() },
			ExtendOwnership: func(context.Context) error { return msg.InProgress() },
		}
		select {
		case deliveries <- delivery:
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
	return &OrderReceiver{deliveries: deliveries}, nil
}

func (r *OrderReceiver) Deliveries() <-chan crawlcapability.DeliveredOrder {
	return r.deliveries
}
