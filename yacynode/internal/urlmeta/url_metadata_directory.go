package urlmeta

import (
	"context"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
)

type urlDirectory struct {
	vault      *vault.Vault
	collection *vault.Collection[yacymodel.URIMetadataRow]
	observers  observers
}

func (d urlDirectory) RowsByHash(
	ctx context.Context,
	hashes []yacymodel.Hash,
) ([]yacymodel.URIMetadataRow, error) {
	rows := make([]yacymodel.URIMetadataRow, 0, len(hashes))
	err := d.vault.View(ctx, func(tx *vault.Txn) error {
		for _, hash := range hashes {
			row, ok, err := d.collection.Get(tx, vault.Key(hash))
			if err != nil {
				return fmt.Errorf("read url metadata: %w", err)
			}
			if !ok {
				continue
			}
			rows = append(rows, row)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("rows by hash: %w", err)
	}

	return rows, nil
}

func (d urlDirectory) MissingURLs(
	ctx context.Context,
	hashes []yacymodel.Hash,
) ([]yacymodel.Hash, error) {
	missing := make([]yacymodel.Hash, 0)
	seen := make(map[yacymodel.Hash]struct{}, len(hashes))
	err := d.vault.View(ctx, func(tx *vault.Txn) error {
		for _, hash := range hashes {
			if _, ok := seen[hash]; ok {
				continue
			}
			seen[hash] = struct{}{}

			_, ok, err := d.collection.Get(tx, vault.Key(hash))
			if err != nil {
				return fmt.Errorf("read url metadata: %w", err)
			}
			if !ok {
				missing = append(missing, hash)
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("missing urls: %w", err)
	}

	return missing, nil
}

func (d urlDirectory) Count(ctx context.Context) (int, error) {
	var count int
	err := d.vault.View(ctx, func(tx *vault.Txn) error {
		length, err := d.collection.Len(tx)
		if err != nil {
			return fmt.Errorf("read url metadata length: %w", err)
		}
		count = length

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("url count: %w", err)
	}

	return count, nil
}

func (d urlDirectory) Purge(
	ctx context.Context,
	tx *vault.Txn,
	urls []yacymodel.Hash,
) (PurgeResult, error) {
	var result PurgeResult
	for _, hash := range urls {
		deleted, err := d.collection.Delete(tx, vault.Key(hash))
		if err != nil {
			return PurgeResult{}, fmt.Errorf("delete url metadata: %w", err)
		}
		if !deleted {
			continue
		}
		d.observers.purged(ctx, tx, hash)
		result.URLsDeleted++
	}

	return result, nil
}

var (
	_ URLDirectory = urlDirectory{}
	_ URLEvictor   = urlDirectory{}
)
