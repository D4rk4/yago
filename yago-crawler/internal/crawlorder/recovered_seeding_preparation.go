package crawlorder

import (
	"context"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func (c *CrawlOrderConsumer) prepareRecoveredSeedingOrder(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	delivery CrawlOrderDelivery,
) (crawladmission.AdmissionProfile, []yagocrawlcontract.CrawlRequest, bool) {
	profile, prepared := c.compileCrawlOrder(ctx, order, delivery)
	if !prepared {
		return crawladmission.AdmissionProfile{}, nil, false
	}
	return profile, nil, true
}
