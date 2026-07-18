package rwi

import (
	"context"
	"errors"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (i postingIntake) busyReceipt() Receipt {
	return Receipt{Busy: true, Pause: i.pauseMilliseconds}
}

func (i postingIntake) storageAtCapacity(ctx context.Context) (bool, error) {
	atCapacity, err := i.vault.AtCapacity(ctx)
	if err == nil {
		return atCapacity, nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true, nil
	}

	return false, fmt.Errorf("check capacity: %w", err)
}

func (i postingIntake) storeEntries(
	ctx context.Context,
	entries []yagomodel.RWIPosting,
) ([]yagomodel.Hash, bool, error) {
	referenced := make([]yagomodel.Hash, 0, len(entries))
	err := i.vault.Update(ctx, func(tx *vault.Txn) error {
		referenced = referenced[:0]
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("context: %w", err)
			}

			urlHash, err := entry.URLHash()
			if err != nil {
				return fmt.Errorf("rwi posting url hash: %w", err)
			}
			hash := urlHash.Hash()

			if err := i.postings.Put(tx, postingKey(entry.WordHash, hash), entry); err != nil {
				return fmt.Errorf("store rwi posting: %w", err)
			}
			if err := i.observers.stored(tx, entry.WordHash, hash); err != nil {
				return fmt.Errorf("note referenced url: %w", err)
			}
			referenced = append(referenced, hash)
		}

		return nil
	})
	if err == nil {
		return referenced, false, nil
	}
	if errors.Is(err, vault.ErrAtCapacity) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return nil, true, nil
	}

	return nil, false, fmt.Errorf("store rwi: %w", err)
}
