package crawlbroker

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (q *DurableOrderQueue) publishAutomaticDiscovery(
	ctx context.Context,
	key string,
	data []byte,
) (bool, error) {
	q.discoveryAdmission.Lock()
	defer q.discoveryAdmission.Unlock()
	if err := q.reconcileAutomaticDiscoveryIntents(ctx); err != nil {
		return false, fmt.Errorf("enqueue crawl order: %w", err)
	}
	for {
		duplicate, err := q.admitAutomaticDiscoveryGrowth(ctx, key)
		if err != nil {
			return false, fmt.Errorf("enqueue crawl order: %w", err)
		}
		if !duplicate {
			break
		}
		active, err := q.reconcileAutomaticDiscoveryKey(ctx, key)
		if err != nil {
			return false, fmt.Errorf("enqueue crawl order: %w", err)
		}
		if active {
			return true, nil
		}
	}
	if err := q.persistAutomaticDiscoveryIntent(ctx, key, data); err != nil {
		return false, fmt.Errorf("enqueue crawl order: %w", err)
	}
	duplicate, err := q.completeAutomaticDiscoveryIntent(
		ctx,
		key,
		data,
		false,
	)
	if err != nil {
		return false, fmt.Errorf("enqueue crawl order: %w", err)
	}
	if !duplicate {
		q.signal()
	}
	if err := q.releaseAutomaticDiscoveryIntent(ctx, key); err != nil {
		return false, fmt.Errorf("enqueue crawl order: %w", err)
	}

	return duplicate, nil
}

func (q *DurableOrderQueue) reconcileAutomaticDiscoveryKey(
	ctx context.Context,
	key string,
) (bool, error) {
	outstanding := false
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		var err error
		outstanding, err = q.automaticDiscoveryOutstandingTx(tx, key, true)

		return err
	}); err != nil {
		return false, fmt.Errorf("repair active crawl discovery key: %w", err)
	}

	return outstanding, nil
}

func (q *DurableOrderQueue) admitAutomaticDiscoveryGrowth(
	ctx context.Context,
	key string,
) (bool, error) {
	duplicate, err := q.automaticDiscoveryOutstanding(ctx, key)
	if err != nil || duplicate || q.growthAdmission == nil {
		return duplicate, err
	}
	if err := q.growthAdmission.CheckGrowth(); err != nil {
		return false, fmt.Errorf("crawl order growth admission: %w", err)
	}

	return false, nil
}

func (q *DurableOrderQueue) automaticDiscoveryOutstanding(
	ctx context.Context,
	key string,
) (bool, error) {
	outstanding := false
	if err := q.vault.View(ctx, func(tx *vault.Txn) error {
		var err error
		outstanding, err = q.automaticDiscoveryOutstandingTx(tx, key, false)

		return err
	}); err != nil {
		return false, fmt.Errorf("read active crawl discovery: %w", err)
	}

	return outstanding, nil
}

func (q *DurableOrderQueue) automaticDiscoveryOutstandingTx(
	tx *vault.Txn,
	key string,
	migrate bool,
) (bool, error) {
	sequence, active, err := q.activeDiscoveryKeys.Get(tx, vault.Key(key))
	if err != nil {
		return false, fmt.Errorf("read active crawl discovery: %w", err)
	}
	if active {
		return q.activeAutomaticDiscoveryOutstandingTx(tx, key, sequence, migrate)
	}
	sequence, legacy, err := q.keys.Get(tx, vault.Key(key))
	if err != nil {
		return false, fmt.Errorf("read legacy crawl discovery: %w", err)
	}
	if !legacy {
		return false, nil
	}
	return q.legacyAutomaticDiscoveryOutstandingTx(tx, key, sequence, migrate)
}

func (q *DurableOrderQueue) untrackedAutomaticDiscoveryTx(
	tx *vault.Txn,
	key string,
	migrate bool,
) (bool, error) {
	sequence, found, err := q.automaticDiscoveryPendingSequenceTx(tx, key)
	if err != nil {
		return false, err
	}
	if found {
		if migrate {
			if err := q.activeDiscoveryKeys.Put(tx, vault.Key(key), sequence); err != nil {
				return false, fmt.Errorf("repair active crawl discovery: %w", err)
			}
			if err := q.pendingDiscoveryKeys.Put(
				tx,
				orderKey(sequence),
				[]byte(key),
			); err != nil {
				return false, fmt.Errorf("repair pending crawl discovery: %w", err)
			}
		}

		return true, nil
	}
	return q.untrackedAutomaticDiscoveryLeaseTx(tx, key, migrate)
}

func (q *DurableOrderQueue) untrackedAutomaticDiscoveryLeaseTx(
	tx *vault.Txn,
	key string,
	migrate bool,
) (bool, error) {
	leaseKey, record, found, err := q.automaticDiscoveryLeaseTx(tx, key)
	if err != nil || !found {
		return found, err
	}
	if migrate {
		record.DiscoveryKey = key
		if err := q.leases.Put(tx, leaseKey, record); err != nil {
			return false, fmt.Errorf("repair leased crawl discovery: %w", err)
		}
		if err := q.recordLeasedAutomaticDiscoveryTx(tx, key, leaseKey); err != nil {
			return false, err
		}
		if err := q.activeDiscoveryKeys.Put(
			tx,
			vault.Key(key),
			record.DiscoverySequence,
		); err != nil {
			return false, fmt.Errorf("repair active leased crawl discovery: %w", err)
		}
	}

	return true, nil
}

func (q *DurableOrderQueue) automaticDiscoveryPendingSequenceTx(
	tx *vault.Txn,
	key string,
) (uint64, bool, error) {
	var sequence uint64
	found := false
	if err := q.automaticOrderIndex.Scan(tx, nil, func(
		orderSequence vault.Key,
		_ []byte,
	) (bool, error) {
		data, pending, err := q.orders.Get(tx, orderSequence)
		if err != nil {
			return false, fmt.Errorf("read untracked crawl discovery: %w", err)
		}
		if !pending {
			return true, nil
		}
		matches, err := automaticDiscoveryOrderMatchesKey(data, key)
		if err != nil {
			return false, err
		}
		if !matches {
			return true, nil
		}
		sequence, err = (sequenceCodec{}).Decode(orderSequence)
		if err != nil {
			return false, fmt.Errorf("decode untracked crawl discovery sequence: %w", err)
		}
		found = true

		return false, nil
	}); err != nil {
		return 0, false, fmt.Errorf("scan untracked crawl discovery: %w", err)
	}

	return sequence, found, nil
}

func (q *DurableOrderQueue) migratePendingAutomaticDiscoveryTx(
	tx *vault.Txn,
	key string,
	sequence uint64,
) error {
	if err := q.activeDiscoveryKeys.Put(tx, vault.Key(key), sequence); err != nil {
		return fmt.Errorf("record migrated crawl discovery: %w", err)
	}
	if err := q.pendingDiscoveryKeys.Put(tx, orderKey(sequence), []byte(key)); err != nil {
		return fmt.Errorf("record migrated pending crawl discovery: %w", err)
	}
	if _, err := q.keys.Delete(tx, vault.Key(key)); err != nil {
		return fmt.Errorf("release legacy crawl discovery key: %w", err)
	}

	return nil
}

func automaticDiscoveryOrderMatchesKey(data []byte, key string) (bool, error) {
	order, err := yagocrawlcontract.UnmarshalCrawlOrder(data)
	if err != nil {
		return false, fmt.Errorf("decode automatic crawl discovery: %w", err)
	}
	if order.Priority != yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery {
		return false, nil
	}
	for _, request := range order.Requests {
		if request.URL == key {
			return true, nil
		}
	}

	return false, nil
}
