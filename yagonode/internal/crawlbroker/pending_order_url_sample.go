package crawlbroker

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const maximumPendingOrderURLSample = 4

func (q *DurableOrderQueue) PendingURLSample(ctx context.Context) ([]string, error) {
	urls := make([]string, 0, maximumPendingOrderURLSample)
	if err := q.vault.View(ctx, func(tx *vault.Txn) error {
		return q.orders.Scan(tx, nil, func(_ vault.Key, data []byte) (bool, error) {
			order, err := yagocrawlcontract.UnmarshalCrawlOrder(data)
			if err != nil {
				return false, fmt.Errorf("decode pending crawl order sample: %w", err)
			}
			for _, request := range order.Requests {
				if request.URL == "" {
					continue
				}
				urls = append(urls, request.URL)
				if len(urls) == maximumPendingOrderURLSample {
					return false, nil
				}
			}
			return true, nil
		})
	}); err != nil {
		return nil, fmt.Errorf("read pending crawl URL sample: %w", err)
	}

	return urls, nil
}
