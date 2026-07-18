package urlmeta

import (
	"context"
	"errors"
	"fmt"

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
	if len(rows) == 0 {
		return Receipt{}, nil
	}
	atCapacity, err := i.vault.AtCapacity(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Receipt{Busy: true}, nil
		}
		return Receipt{}, fmt.Errorf("check capacity: %w", err)
	}
	if atCapacity {
		return Receipt{Busy: true}, nil
	}

	var existing, rejected []yagomodel.Hash
	var discards []urlRowDiscard
	var observerFailures []urlObserverFailure

	err = i.vault.Update(ctx, func(tx *vault.Txn) error {
		var storeErr error
		existing, rejected, discards, observerFailures, storeErr = i.store(ctx, tx, rows)

		return storeErr
	})
	if errors.Is(err, vault.ErrAtCapacity) {
		return Receipt{Busy: true}, nil
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Receipt{Busy: true}, nil
		}
		return Receipt{}, fmt.Errorf("store urls: %w", err)
	}
	logURLRowDiscards(ctx, discards)
	logURLObserverFailures(ctx, observerFailures)

	return Receipt{
		Double:      len(existing),
		ExistingURL: existing,
		ErrorURL:    rejected,
	}, nil
}

func (i urlIntake) store(
	ctx context.Context,
	tx *vault.Txn,
	rows []yagomodel.URIMetadataRow,
) (
	existing, rejected []yagomodel.Hash,
	discards []urlRowDiscard,
	observerFailures []urlObserverFailure,
	err error,
) {
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("context: %w", err)
		}

		hash, err := row.URLHash()
		if err != nil {
			discards = append(discards, urlRowDiscard{reason: urlDiscardInvalidHash, err: err})

			continue
		}

		key := vault.Key(hash.Hash())
		_, found, err := i.collection.Get(tx, key)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("read url metadata: %w", err)
		}
		if found {
			existing = append(existing, hash.Hash())

			continue
		}
		if err := i.collection.Put(tx, key, row); err != nil {
			if errors.Is(err, vault.ErrCollectionMutationIncomplete) ||
				errors.Is(err, vault.ErrContended) {
				return nil, nil, nil, nil, fmt.Errorf("store url metadata: %w", err)
			}
			rejected = append(rejected, hash.Hash())
			discards = append(discards, urlRowDiscard{reason: urlDiscardStoreFailed, err: err})

			continue
		}
		observerFailures = append(
			observerFailures,
			i.observers.stored(tx, hash.Hash(), row.Freshness())...,
		)
	}

	return existing, rejected, discards, observerFailures, nil
}
