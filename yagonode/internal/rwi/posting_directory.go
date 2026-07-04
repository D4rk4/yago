package rwi

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type postingDirectory struct {
	vault            *vault.Vault
	postings         *vault.Collection[yagomodel.RWIPosting]
	outboundSelected *vault.Collection[yagomodel.RWIPosting]
	observers        postingObservers
}

func (d postingDirectory) RWICount(ctx context.Context) (int, error) {
	return collectionLength(ctx, d.vault, d.postings)
}

func (d postingDirectory) RWIURLCount(ctx context.Context, word yagomodel.Hash) (int, error) {
	var count int
	err := d.ScanWord(ctx, word, func(yagomodel.RWIPosting) (bool, error) {
		count++

		return true, nil
	})
	if err != nil {
		return 0, fmt.Errorf("count rwi word urls: %w", err)
	}

	return count, nil
}

func (d postingDirectory) PurgePosting(
	tx *vault.Txn,
	word, url yagomodel.Hash,
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
	word yagomodel.Hash,
	visit func(yagomodel.RWIPosting) (bool, error),
) error {
	err := d.vault.View(ctx, func(tx *vault.Txn) error {
		return d.postings.Scan(
			tx,
			vault.Key(word),
			func(_ vault.Key, entry yagomodel.RWIPosting) (bool, error) {
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
