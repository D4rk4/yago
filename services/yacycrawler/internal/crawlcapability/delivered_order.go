package crawlcapability

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

type DeliveredOrder struct {
	Order           yacycrawlcontract.CrawlOrder
	Ack             func(context.Context) error
	Retry           func(context.Context) error
	ExtendOwnership func(context.Context) error
}
