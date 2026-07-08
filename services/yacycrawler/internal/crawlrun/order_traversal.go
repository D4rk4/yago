package crawlrun

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
)

type OrderTraversal interface {
	Traverse(ctx context.Context, delivery crawlcapability.DeliveredOrder) error
}
