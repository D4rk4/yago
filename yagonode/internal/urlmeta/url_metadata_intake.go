package urlmeta

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const urlRowDiscarded = "url row discarded"

type urlIntake struct {
	vault      *vault.Vault
	collection *vault.Collection[yagomodel.URIMetadataRow]
	observers  observers
}

func (i urlIntake) Receive(
	ctx context.Context,
	rows []yagomodel.URIMetadataRow,
) (Receipt, error) {
	atCapacity, err := i.vault.AtCapacity(ctx)
	if err != nil {
		return Receipt{}, fmt.Errorf("check capacity: %w", err)
	}
	if atCapacity {
		return Receipt{Busy: true}, nil
	}

	var existing, rejected []yagomodel.Hash

	err = i.vault.Update(ctx, func(tx *vault.Txn) error {
		var storeErr error
		existing, rejected, storeErr = i.store(ctx, tx, rows)

		return storeErr
	})
	if errors.Is(err, vault.ErrAtCapacity) {
		return Receipt{Busy: true}, nil
	}
	if err != nil {
		return Receipt{}, fmt.Errorf("store urls: %w", err)
	}

	return Receipt{Double: len(existing), ErrorURL: rejected}, nil
}

func (i urlIntake) store(
	ctx context.Context,
	tx *vault.Txn,
	rows []yagomodel.URIMetadataRow,
) (existing, rejected []yagomodel.Hash, err error) {
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, nil, fmt.Errorf("context: %w", err)
		}

		hash, err := row.URLHash()
		if err != nil {
			slog.WarnContext(ctx, urlRowDiscarded,
				slog.String("reason", "invalid url hash"),
				slog.Any("error", err),
			)

			continue
		}

		key := vault.Key(hash.Hash())
		_, found, err := i.collection.Get(tx, key)
		if err != nil {
			return nil, nil, fmt.Errorf("read url metadata: %w", err)
		}
		if found {
			existing = append(existing, hash.Hash())

			continue
		}
		if err := i.collection.Put(tx, key, row); err != nil {
			// A contended shard is a retryable abort: returning it lets the
			// engine re-run this whole update under its exclusive gate
			// (STOR-05). Swallowing it here silently dropped inbound rows.
			if errors.Is(err, vault.ErrContended) {
				return nil, nil, fmt.Errorf("store url metadata: %w", err)
			}
			rejected = append(rejected, hash.Hash())
			slog.WarnContext(ctx, urlRowDiscarded,
				slog.String("reason", "store failed"),
				slog.Any("error", err),
			)

			continue
		}
		i.observers.stored(ctx, tx, hash.Hash(), row.Freshness())
	}

	return existing, rejected, nil
}
