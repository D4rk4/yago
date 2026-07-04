package crawlorder

import (
	"context"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type CrawlOrderDelivery struct {
	Order yagocrawlcontract.CrawlOrder
	Ack   func(context.Context) error
	Nak   func(context.Context) error
	Term  func(context.Context) error
}
