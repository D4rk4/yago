package urlmetastaleness

import (
	"context"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
)

const (
	orderBucket     boltvault.Name = "urlmeta_staleness_order"
	freshnessBucket boltvault.Name = "urlmeta_staleness_freshness"
)

type stalenessRanking struct {
	vault     *boltvault.Vault
	order     *boltvault.Collection[struct{}]
	freshness *boltvault.Collection[string]
}

func openStalenessRanking(vault *boltvault.Vault) (*stalenessRanking, error) {
	order, err := boltvault.Register(vault, orderBucket, presenceCodec{})
	if err != nil {
		return nil, fmt.Errorf("register staleness order: %w", err)
	}
	freshness, err := boltvault.Register(vault, freshnessBucket, freshnessCodec{})
	if err != nil {
		return nil, fmt.Errorf("register staleness freshness: %w", err)
	}

	return &stalenessRanking{vault: vault, order: order, freshness: freshness}, nil
}

func (o *stalenessRanking) StalestURLs(ctx context.Context, limit int) ([]yacymodel.Hash, error) {
	if limit <= 0 {
		return nil, nil
	}

	stalest := make([]yacymodel.Hash, 0, limit)
	err := o.vault.View(ctx, func(tx *boltvault.Txn) error {
		return o.order.Scan(
			tx,
			nil,
			func(key boltvault.Key, _ struct{}) (bool, error) {
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
	tx *boltvault.Txn,
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
	if err := o.freshness.Put(tx, boltvault.Key(hash), freshness); err != nil {
		return fmt.Errorf("record staleness freshness: %w", err)
	}

	return nil
}

var _ StalenessRanking = (*stalenessRanking)(nil)

func (o *stalenessRanking) URLPurged(tx *boltvault.Txn, hash yacymodel.Hash) error {
	freshness, found, err := o.freshness.Get(tx, boltvault.Key(hash))
	if err != nil {
		return fmt.Errorf("read staleness freshness: %w", err)
	}
	if !found {
		return nil
	}
	if _, err := o.order.Delete(
		tx,
		rankedURL{freshness: freshness, hash: hash}.orderKey(),
	); err != nil {
		return fmt.Errorf("drop staleness order: %w", err)
	}
	if _, err := o.freshness.Delete(tx, boltvault.Key(hash)); err != nil {
		return fmt.Errorf("drop staleness freshness: %w", err)
	}

	return nil
}
