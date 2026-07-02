package urlmetastaleness

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

const (
	orderBucket     vault.Name = "urlmeta_staleness_order"
	freshnessBucket vault.Name = "urlmeta_staleness_freshness"
)

type stalenessRanking struct {
	vault     *vault.Vault
	order     *vault.Collection[struct{}]
	freshness *vault.Collection[string]
}

func openStalenessRanking(v *vault.Vault) (*stalenessRanking, error) {
	order, err := vault.Register(v, orderBucket, presenceCodec{})
	if err != nil {
		return nil, fmt.Errorf("register staleness order: %w", err)
	}
	freshness, err := vault.Register(v, freshnessBucket, freshnessCodec{})
	if err != nil {
		return nil, fmt.Errorf("register staleness freshness: %w", err)
	}

	return &stalenessRanking{vault: v, order: order, freshness: freshness}, nil
}

func (o *stalenessRanking) StalestURLs(ctx context.Context, limit int) ([]yacymodel.Hash, error) {
	if limit <= 0 {
		return nil, nil
	}

	stalest := make([]yacymodel.Hash, 0, limit)
	err := o.vault.View(ctx, func(tx *vault.Txn) error {
		return o.order.Scan(
			tx,
			nil,
			func(key vault.Key, _ struct{}) (bool, error) {
				hash, err := hashFromOrderKey(key)
				if err != nil {
					return false, err
				}
				stalest = append(stalest, hash)

				return len(stalest) < limit, nil
			},
		)
	})
	if err != nil {
		return nil, fmt.Errorf("select stalest urls: %w", err)
	}

	return stalest, nil
}

func (o *stalenessRanking) URLStored(
	tx *vault.Txn,
	hash yacymodel.Hash,
	freshness string,
) error {
	if err := o.order.Put(
		tx,
		rankedURL{freshness: freshness, hash: hash}.orderKey(),
		struct{}{},
	); err != nil {
		return fmt.Errorf("record staleness order: %w", err)
	}
	if err := o.freshness.Put(tx, vault.Key(hash), freshness); err != nil {
		return fmt.Errorf("record staleness freshness: %w", err)
	}

	return nil
}

var _ StalenessRanking = (*stalenessRanking)(nil)

func (o *stalenessRanking) URLPurged(tx *vault.Txn, hash yacymodel.Hash) error {
	freshness, found, _ := o.freshness.Get(tx, vault.Key(hash))
	if !found {
		return nil
	}
	if _, err := o.order.Delete(
		tx,
		rankedURL{freshness: freshness, hash: hash}.orderKey(),
	); err != nil {
		return fmt.Errorf("drop staleness order: %w", err)
	}
	if _, err := o.freshness.Delete(tx, vault.Key(hash)); err != nil {
		return fmt.Errorf("drop staleness freshness: %w", err)
	}

	return nil
}
