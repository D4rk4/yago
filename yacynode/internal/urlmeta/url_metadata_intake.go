package urlmeta

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
)

const urlRowDiscarded = "url row discarded"

type urlIntake struct {
	vault      *boltvault.Vault
	collection *boltvault.Collection[yacymodel.URIMetadataRow]
}

func (i urlIntake) Receive(
	ctx context.Context,
	rows []yacymodel.URIMetadataRow,
) (Receipt, error) {
	atCapacity, err := i.vault.AtCapacity(ctx)
	if err != nil {
		return Receipt{}, fmt.Errorf("check capacity: %w", err)
	}
	if atCapacity {
		return Receipt{Busy: true}, nil
	}

	var existing, rejected []yacymodel.Hash

	err = i.vault.Update(ctx, func(tx *boltvault.Txn) error {
		var storeErr error
		existing, rejected, storeErr = i.store(ctx, tx, rows)

		return storeErr
	})
	if errors.Is(err, boltvault.ErrAtCapacity) {
		return Receipt{Busy: true}, nil
	}
	if err != nil {
		return Receipt{}, fmt.Errorf("store urls: %w", err)
	}

	return Receipt{Double: len(existing), ErrorURL: rejected}, nil
}

func (i urlIntake) store(
	ctx context.Context,
	tx *boltvault.Txn,
	rows []yacymodel.URIMetadataRow,
) (existing, rejected []yacymodel.Hash, err error) {
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

		key := boltvault.Key(hash.Hash())
		_, found, err := i.collection.Get(tx, key)
		if err != nil {
			return nil, nil, fmt.Errorf("read url metadata: %w", err)
		}
		if found {
			existing = append(existing, hash.Hash())

			continue
		}
		if err := i.collection.Put(tx, key, row); err != nil {
			rejected = append(rejected, hash.Hash())
			slog.WarnContext(ctx, urlRowDiscarded,
				slog.String("reason", "store failed"),
				slog.Any("error", err),
			)
		}
	}

	return existing, rejected, nil
}
