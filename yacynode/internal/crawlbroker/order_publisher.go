package crawlbroker

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/D4rk4/yago/yacycrawlcontract"
)

type OrderPublisher struct {
	js      jetstream.JetStream
	subject string
}

type crawlOrderPublisher interface {
	Publish(context.Context, string, []byte, ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

var (
	marshalCrawlOrder = yacycrawlcontract.MarshalCrawlOrder
	publishCrawlOrder = func(
		ctx context.Context,
		js crawlOrderPublisher,
		subject string,
		data []byte,
	) error {
		_, err := js.Publish(ctx, subject, data)
		if err != nil {
			return fmt.Errorf("publish crawl order: %w", err)
		}

		return nil
	}
)

func newOrderPublisher(js jetstream.JetStream, subject string) *OrderPublisher {
	return &OrderPublisher{js: js, subject: subject}
}

func (p *OrderPublisher) Publish(ctx context.Context, order yacycrawlcontract.CrawlOrder) error {
	data, err := marshalCrawlOrder(order)
	if err != nil {
		return fmt.Errorf("encode crawl order: %w", err)
	}
	if err := publishCrawlOrder(ctx, p.js, p.subject, data); err != nil {
		return fmt.Errorf("publish crawl order: %w", err)
	}
	return nil
}
