package crawlbroker

import (
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (q *DurableOrderQueue) legacyAutomaticDiscoveryOutstandingTx(
	tx *vault.Txn,
	key string,
	sequence uint64,
	migrate bool,
) (bool, error) {
	data, pending, err := q.orders.Get(tx, orderKey(sequence))
	if err != nil {
		return false, fmt.Errorf("read legacy pending crawl discovery: %w", err)
	}
	if pending {
		matches, err := automaticDiscoveryOrderMatchesKey(data, key)
		if err != nil || !matches {
			return false, err
		}
		if migrate {
			if err := q.migratePendingAutomaticDiscoveryTx(tx, key, sequence); err != nil {
				return false, err
			}
		}

		return true, nil
	}
	leaseKey, record, found, err := q.automaticDiscoveryLeaseTx(tx, key)
	if err != nil {
		return false, err
	}
	if !found {
		return q.untrackedAutomaticDiscoveryTx(tx, key, migrate)
	}
	if migrate {
		if err := q.migrateLegacyAutomaticDiscoveryLeaseTx(
			tx,
			key,
			leaseKey,
			record,
		); err != nil {
			return false, err
		}
	}

	return true, nil
}

func (q *DurableOrderQueue) migrateLegacyAutomaticDiscoveryLeaseTx(
	tx *vault.Txn,
	key string,
	leaseKey vault.Key,
	record leaseRecord,
) error {
	record.DiscoveryKey = key
	if err := q.leases.Put(tx, leaseKey, record); err != nil {
		return fmt.Errorf("migrate leased crawl discovery: %w", err)
	}
	if err := q.recordLeasedAutomaticDiscoveryTx(tx, key, leaseKey); err != nil {
		return err
	}
	if err := q.activeDiscoveryKeys.Put(
		tx,
		vault.Key(key),
		record.DiscoverySequence,
	); err != nil {
		return fmt.Errorf("record migrated crawl discovery: %w", err)
	}

	return nil
}
