package rwi

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

type postingDirectory struct {
	vault            *vault.Vault
	postings         *vault.Collection[yacymodel.RWIPosting]
	outboundSelected *vault.Collection[yacymodel.RWIPosting]
	observers        postingObservers
}

func (d postingDirectory) RWICount(ctx context.Context) (int, error) {
	return collectionLength(ctx, d.vault, d.postings)
}

func (d postingDirectory) PurgePosting(
	tx *vault.Txn,
	word, url yacymodel.Hash,
) (bool, error) {
	deleted, err := d.postings.Delete(tx, postingKey(word, url))
	if err != nil {
		return false, fmt.Errorf("delete rwi posting: %w", err)
	}
	if err := d.observers.purged(tx, word, url); err != nil {
		return false, err
	}

	return deleted, nil
}

func (d postingDirectory) ScanWord(
	ctx context.Context,
	word yacymodel.Hash,
	visit func(yacymodel.RWIPosting) (bool, error),
) error {
	err := d.vault.View(ctx, func(tx *vault.Txn) error {
		return d.postings.Scan(
			tx,
			vault.Key(word),
			func(_ vault.Key, entry yacymodel.RWIPosting) (bool, error) {
				if err := ctx.Err(); err != nil {
					return false, fmt.Errorf("context: %w", err)
				}
				entry.WordHash = word

				return visit(entry)
			},
		)
	})
	if err != nil {
		return fmt.Errorf("scan word postings: %w", err)
	}

	return nil
}

func collectionLength[V any](
	ctx context.Context,
	v *vault.Vault,
	collection *vault.Collection[V],
) (int, error) {
	var length int
	err := v.View(ctx, func(tx *vault.Txn) error {
		measured, err := collection.Len(tx)
		if err != nil {
			return fmt.Errorf("read length: %w", err)
		}
		length = measured

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("collection length: %w", err)
	}

	return length, nil
}

var (
	_ PostingIndex  = postingDirectory{}
	_ PostingPurger = postingDirectory{}
)
