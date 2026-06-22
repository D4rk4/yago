package rwi

import (
	"context"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
)

type postingDirectory struct {
	vault      *boltvault.Vault
	postings   *boltvault.Collection[yacymodel.RWIPosting]
	references *boltvault.Collection[struct{}]
}

func (d postingDirectory) RWICount(ctx context.Context) (int, error) {
	return collectionLength(ctx, d.vault, d.postings)
}

func (d postingDirectory) ReferencedURLCount(ctx context.Context) (int, error) {
	return collectionLength(ctx, d.vault, d.references)
}

func (d postingDirectory) PurgeReferences(
	tx *boltvault.Txn,
	urls []yacymodel.Hash,
) (PurgeResult, error) {
	targets := make(map[yacymodel.Hash]struct{}, len(urls))
	for _, hash := range urls {
		targets[hash] = struct{}{}
	}

	var stale []boltvault.Key
	err := d.postings.Scan(tx, nil, func(key boltvault.Key, _ yacymodel.RWIPosting) (bool, error) {
		id, parseErr := parsePostingKey(key)
		if parseErr == nil {
			if _, ok := targets[id.URLHash]; ok {
				stale = append(stale, key)
			}
		}

		return true, nil
	})
	if err != nil {
		return PurgeResult{}, fmt.Errorf("scan rwi postings: %w", err)
	}

	var result PurgeResult
	for _, key := range stale {
		deleted, err := d.postings.Delete(tx, key)
		if err != nil {
			return PurgeResult{}, fmt.Errorf("delete rwi posting: %w", err)
		}
		if deleted {
			result.PostingsDeleted++
		}
	}
	for _, hash := range urls {
		deleted, err := d.references.Delete(tx, boltvault.Key(hash))
		if err != nil {
			return PurgeResult{}, fmt.Errorf("delete referenced url: %w", err)
		}
		if deleted {
			result.ReferencesDeleted++
		}
	}

	return result, nil
}

func (d postingDirectory) ScanWord(
	ctx context.Context,
	word yacymodel.Hash,
	visit func(yacymodel.RWIPosting) (bool, error),
) error {
	err := d.vault.View(ctx, func(tx *boltvault.Txn) error {
		return d.postings.Scan(
			tx,
			boltvault.Key(word),
			func(_ boltvault.Key, entry yacymodel.RWIPosting) (bool, error) {
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
	vault *boltvault.Vault,
	collection *boltvault.Collection[V],
) (int, error) {
	var length int
	err := vault.View(ctx, func(tx *boltvault.Txn) error {
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
	_ PostingDirectory = postingDirectory{}
	_ PostingScanner   = postingDirectory{}
)
