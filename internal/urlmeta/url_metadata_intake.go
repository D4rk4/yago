package urlmeta

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/nikitakarpei/yacy-rwi-node/internal/boltvault"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const urlRowDiscarded = "url row discarded"

type Receipt struct {
	Busy     bool
	Double   int
	ErrorURL []yacymodel.Hash
}

type urlIntake struct {
	vault      *boltvault.Vault
	collection *boltvault.Collection[yacymodel.URIMetadataRow]
}

func (i urlIntake) Receive(
	ctx context.Context,
	rows []yacymodel.URIMetadataRow,
) (Receipt, error) {
	var existing, rejected []yacymodel.Hash

	err := i.vault.Update(ctx, func(tx *boltvault.Txn) error {
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("context: %w", err)
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
				return fmt.Errorf("read url metadata: %w", err)
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

		return nil
	})
	if errors.Is(err, boltvault.ErrAtCapacity) {
		return Receipt{Busy: true}, nil
	}
	if err != nil {
		return Receipt{}, fmt.Errorf("store urls: %w", err)
	}

	return Receipt{Double: len(existing), ErrorURL: rejected}, nil
}
