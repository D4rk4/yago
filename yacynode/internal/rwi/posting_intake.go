package rwi

import (
	"context"
	"errors"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmeta"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
)

type postingIntake struct {
	vault        *vault.Vault
	postings     *vault.Collection[yacymodel.RWIPosting]
	observers    postingObservers
	urls         urlmeta.URLDirectory
	batchCap     int
	pauseSeconds int
}

func (i postingIntake) Receive(
	ctx context.Context,
	entries []yacymodel.RWIPosting,
) (Receipt, error) {
	if len(entries) > i.batchCap {
		return Receipt{Busy: true, Pause: i.pauseSeconds}, nil
	}

	atCapacity, err := i.vault.AtCapacity(ctx)
	if err != nil {
		return Receipt{}, fmt.Errorf("check capacity: %w", err)
	}
	if atCapacity {
		return Receipt{Busy: true, Pause: i.pauseSeconds}, nil
	}

	referenced := make([]yacymodel.Hash, 0, len(entries))
	err = i.vault.Update(ctx, func(tx *vault.Txn) error {
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
	if errors.Is(err, vault.ErrAtCapacity) {
		return Receipt{Busy: true, Pause: i.pauseSeconds}, nil
	}
	if err != nil {
		return Receipt{}, fmt.Errorf("store rwi: %w", err)
	}

	unknown, err := i.urls.MissingURLs(ctx, referenced)
	if err != nil {
		return Receipt{}, fmt.Errorf("missing urls: %w", err)
	}

	return Receipt{UnknownURL: unknown}, nil
}
