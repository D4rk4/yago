package crawlbroker

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

type OrderPublisher struct {
	js      jetstream.JetStream
	subject string
}

func newOrderPublisher(js jetstream.JetStream, subject string) *OrderPublisher {
	return &OrderPublisher{js: js, subject: subject}
}

func (p *OrderPublisher) Publish(ctx context.Context, order yacycrawlcontract.CrawlOrder) error {
	data, err := yacycrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		return fmt.Errorf("encode crawl order: %w", err)
	}
	if _, err := p.js.Publish(ctx, p.subject, data); err != nil {
		return fmt.Errorf("publish crawl order: %w", err)
	}
	return nil
}
