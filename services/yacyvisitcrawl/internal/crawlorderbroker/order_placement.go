package crawlorderbroker

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

type OrderPlacement struct {
	js      jetstream.JetStream
	subject string
}

func newOrderPlacement(js jetstream.JetStream, subject string) *OrderPlacement {
	return &OrderPlacement{js: js, subject: subject}
}

func (p *OrderPlacement) Place(ctx context.Context, order yacycrawlcontract.CrawlOrder) error {
	data, err := yacycrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		return fmt.Errorf("encode crawl order: %w", err)
	}
	if _, err := p.js.Publish(ctx, p.subject, data); err != nil {
		return fmt.Errorf("place crawl order: %w", err)
	}
	return nil
}
