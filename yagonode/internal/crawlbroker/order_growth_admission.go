package crawlbroker

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (q *DurableOrderQueue) admitOrderGrowth(
	ctx context.Context,
	key string,
) (bool, error) {
	duplicate, err := q.orderKeyAccepted(ctx, key)
	if err != nil || duplicate || q.growthAdmission == nil {
		return duplicate, err
	}
	if err := q.growthAdmission.CheckGrowth(); err != nil {
		duplicate, duplicateErr := q.orderKeyAccepted(ctx, key)
		if duplicateErr != nil {
			return false, duplicateErr
		}
		if duplicate {
			return true, nil
		}

		return false, fmt.Errorf("crawl order growth admission: %w", err)
	}

	return false, nil
}

func (q *DurableOrderQueue) orderKeyAccepted(
	ctx context.Context,
	key string,
) (bool, error) {
	if key == "" {
		return false, nil
	}
	accepted := false
	if err := q.vault.View(ctx, func(tx *vault.Txn) error {
		_, found, err := q.keys.Get(tx, vault.Key(key))
		if err != nil {
			return fmt.Errorf("read idempotency key record: %w", err)
		}
		accepted = found

		return nil
	}); err != nil {
		return false, fmt.Errorf("read idempotency key: %w", err)
	}

	return accepted, nil
}
