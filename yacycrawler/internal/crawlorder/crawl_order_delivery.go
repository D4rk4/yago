package crawlorder

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

type CrawlOrderDelivery struct {
	Order      yacycrawlcontract.CrawlOrder
	Ack        func(context.Context) error
	Nak        func(context.Context) error
	Term       func(context.Context) error
	InProgress func(context.Context) error
}
