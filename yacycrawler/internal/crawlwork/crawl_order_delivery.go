package crawlwork

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

type CrawlOrderDelivery struct {
	Order yacycrawlcontract.CrawlOrder
	Ack   func(context.Context) error
	Nak   func(context.Context) error
	Term  func(context.Context) error
}

func NewCrawlOrderDelivery(order yacycrawlcontract.CrawlOrder) CrawlOrderDelivery {
	return CrawlOrderDelivery{
		Order: order,
		Ack:   noopDeliveryAction,
		Nak:   noopDeliveryAction,
		Term:  noopDeliveryAction,
	}
}

func noopDeliveryAction(context.Context) error {
	return nil
}
