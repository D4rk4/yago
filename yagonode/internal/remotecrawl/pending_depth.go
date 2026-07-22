package remotecrawl

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (b *Broker) PendingCount(ctx context.Context) (int, error) {
	count := 0
	if err := b.storage.View(ctx, func(tx *vault.Txn) error {
		pending, err := b.pending.Len(tx)
		if err != nil {
			return fmt.Errorf("count remote crawl pending work: %w", err)
		}
		count = pending

		return nil
	}); err != nil {
		return 0, fmt.Errorf("read remote crawl pending work: %w", err)
	}

	return count, nil
}
