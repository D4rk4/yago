package urlmeta

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type urlDirectory struct {
	vault      *vault.Vault
	collection *vault.Collection[yagomodel.URIMetadataRow]
	observers  observers
}

func (d urlDirectory) RowsByHash(
	ctx context.Context,
	hashes []yagomodel.Hash,
) ([]yagomodel.URIMetadataRow, error) {
	rows := make([]yagomodel.URIMetadataRow, 0, len(hashes))
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
	hashes []yagomodel.Hash,
) ([]yagomodel.Hash, error) {
	missing := make([]yagomodel.Hash, 0)
	seen := make(map[yagomodel.Hash]struct{}, len(hashes))
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

func (d urlDirectory) StoredURLMetadataRows(
	ctx context.Context,
	visit func(yagomodel.URIMetadataRow) (bool, error),
) error {
	err := d.vault.View(ctx, func(tx *vault.Txn) error {
		return d.collection.Scan(
			tx,
			nil,
			func(_ vault.Key, row yagomodel.URIMetadataRow) (bool, error) {
				if err := ctx.Err(); err != nil {
					return false, fmt.Errorf("context: %w", err)
				}

				again, err := visit(row)
				if err != nil {
					return false, err
				}

				return again, nil
			},
		)
	})
	if err != nil {
		return fmt.Errorf("stored url metadata rows: %w", err)
	}

	return nil
}

func (d urlDirectory) Purge(
	ctx context.Context,
	tx *vault.Txn,
	urls []yagomodel.Hash,
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
		result.observerFailures = append(
			result.observerFailures,
			d.observers.purged(tx, hash)...,
		)
		result.URLsDeleted++
	}

	return result, nil
}

var (
	_ URLDirectory          = urlDirectory{}
	_ StoredURLMetadataRows = urlDirectory{}
	_ URLEvictor            = urlDirectory{}
)
