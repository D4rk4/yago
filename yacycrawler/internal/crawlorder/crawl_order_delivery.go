package crawlorder

import (
	"context"

	"github.com/D4rk4/yago/yacycrawlcontract"
)

type CrawlOrderDelivery struct {
	Order yacycrawlcontract.CrawlOrder
	Ack   func(context.Context) error
	Nak   func(context.Context) error
	Term  func(context.Context) error
}
