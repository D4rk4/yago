package crawlbroker

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// QueueDepth is the crawl order backlog held by the broker: Pending orders await
// a worker lease, while Leased orders are in flight with a worker until acked.
type QueueDepth struct {
	Pending int
	Leased  int
}

// Outstanding is the total crawl work the broker holds, whether waiting for a
// worker or already leased to one, so a single scalar reflects work in progress
// rather than dropping to zero the moment an order is leased.
func (d QueueDepth) Outstanding() int {
	return d.Pending + d.Leased
}

// Depth counts the crawl order backlog in a read-only transaction, separating the
// pending FIFO from the leased in-flight orders.
func (q *DurableOrderQueue) Depth(ctx context.Context) (QueueDepth, error) {
	var depth QueueDepth
	if err := q.vault.View(ctx, func(tx *vault.Txn) error {
		pending, err := q.orders.Len(tx)
		if err != nil {
			return fmt.Errorf("count pending crawl orders: %w", err)
		}
		leased, err := q.leases.Len(tx)
		if err != nil {
			return fmt.Errorf("count leased crawl orders: %w", err)
		}
		depth = QueueDepth{Pending: pending, Leased: leased}

		return nil
	}); err != nil {
		return QueueDepth{}, fmt.Errorf("read crawl queue depth: %w", err)
	}

	return depth, nil
}
