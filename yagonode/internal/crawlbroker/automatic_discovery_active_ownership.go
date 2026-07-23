package crawlbroker

import (
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (q *DurableOrderQueue) activeAutomaticDiscoveryOutstandingTx(
	tx *vault.Txn,
	key string,
	sequence uint64,
	migrate bool,
) (bool, error) {
	pending, err := q.activeAutomaticDiscoveryPendingTx(tx, key, sequence, migrate)
	if err != nil || pending {
		return pending, err
	}
	leaseKey, record, found, err := q.automaticDiscoveryLeaseTx(tx, key)
	if err != nil {
		return false, err
	}
	if found {
		if migrate {
			if err := q.repairActiveAutomaticDiscoveryLeaseTx(
				tx,
				key,
				sequence,
				leaseKey,
				record,
			); err != nil {
				return false, err
			}
		}

		return true, nil
	}
	if migrate {
		if err := q.releaseOrphanAutomaticDiscoveryTx(tx, key, sequence); err != nil {
			return false, err
		}
	}

	return q.untrackedAutomaticDiscoveryTx(tx, key, migrate)
}

func (q *DurableOrderQueue) activeAutomaticDiscoveryPendingTx(
	tx *vault.Txn,
	key string,
	sequence uint64,
	migrate bool,
) (bool, error) {
	data, pending, err := q.orders.Get(tx, orderKey(sequence))
	if err != nil {
		return false, fmt.Errorf("read active pending crawl discovery: %w", err)
	}
	if !pending {
		return false, nil
	}
	matches, err := automaticDiscoveryOrderMatchesKey(data, key)
	if err != nil || !matches {
		return false, err
	}
	if migrate {
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

func (q *DurableOrderQueue) repairActiveAutomaticDiscoveryLeaseTx(
	tx *vault.Txn,
	key string,
	sequence uint64,
	leaseKey vault.Key,
	record leaseRecord,
) error {
	if record.DiscoveryKey != key || record.DiscoverySequence != sequence {
		record.DiscoveryKey = key
		record.DiscoverySequence = sequence
		if err := q.leases.Put(tx, leaseKey, record); err != nil {
			return fmt.Errorf("repair leased crawl discovery: %w", err)
		}
	}
	if err := q.recordLeasedAutomaticDiscoveryTx(tx, key, leaseKey); err != nil {
		return err
	}

	return nil
}

func (q *DurableOrderQueue) releaseOrphanAutomaticDiscoveryTx(
	tx *vault.Txn,
	key string,
	sequence uint64,
) error {
	if _, err := q.activeDiscoveryKeys.Delete(tx, vault.Key(key)); err != nil {
		return fmt.Errorf("release orphan crawl discovery: %w", err)
	}
	if _, err := q.pendingDiscoveryKeys.Delete(tx, orderKey(sequence)); err != nil {
		return fmt.Errorf("release orphan pending crawl discovery: %w", err)
	}
	if _, err := q.leasedDiscoveryKeys.Delete(tx, vault.Key(key)); err != nil {
		return fmt.Errorf("release orphan leased crawl discovery: %w", err)
	}

	return nil
}
