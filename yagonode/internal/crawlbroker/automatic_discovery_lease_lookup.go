package crawlbroker

import (
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (q *DurableOrderQueue) automaticDiscoveryLeaseTx(
	tx *vault.Txn,
	key string,
) (vault.Key, leaseRecord, bool, error) {
	indexedLeaseID, indexed, err := q.leasedDiscoveryKeys.Get(tx, vault.Key(key))
	if err != nil {
		return nil, leaseRecord{}, false, fmt.Errorf(
			"read crawl discovery lease key: %w",
			err,
		)
	}
	if indexed {
		leaseKey, record, found, err := q.indexedAutomaticDiscoveryLeaseTx(
			tx,
			key,
			indexedLeaseID,
		)
		if err != nil || found {
			return leaseKey, record, found, err
		}
	}

	return q.scanAutomaticDiscoveryLeaseTx(tx, key)
}

func (q *DurableOrderQueue) indexedAutomaticDiscoveryLeaseTx(
	tx *vault.Txn,
	key string,
	indexedLeaseID []byte,
) (vault.Key, leaseRecord, bool, error) {
	leaseKey := vault.Key(indexedLeaseID)
	record, found, err := q.leases.Get(tx, leaseKey)
	if err != nil {
		return nil, leaseRecord{}, false, fmt.Errorf(
			"read indexed crawl discovery lease: %w",
			err,
		)
	}
	if !found {
		return nil, leaseRecord{}, false, nil
	}
	if record.DiscoveryKey == key {
		return leaseKey, record, true, nil
	}
	matches, err := automaticDiscoveryOrderMatchesKey(record.OrderData, key)
	if err != nil || !matches {
		return nil, leaseRecord{}, false, err
	}

	return leaseKey, record, true, nil
}

func (q *DurableOrderQueue) scanAutomaticDiscoveryLeaseTx(
	tx *vault.Txn,
	key string,
) (vault.Key, leaseRecord, bool, error) {
	var matchedLeaseKey vault.Key
	var matchedLease leaseRecord
	if err := q.leases.Scan(tx, nil, func(
		leaseKey vault.Key,
		record leaseRecord,
	) (bool, error) {
		matches := record.DiscoveryKey == key
		if !matches {
			var err error
			matches, err = automaticDiscoveryOrderMatchesKey(record.OrderData, key)
			if err != nil {
				return false, err
			}
		}
		if !matches {
			return true, nil
		}
		matchedLeaseKey = append(vault.Key(nil), leaseKey...)
		matchedLease = record

		return false, nil
	}); err != nil {
		return nil, leaseRecord{}, false, fmt.Errorf(
			"scan crawl discovery leases: %w",
			err,
		)
	}

	return matchedLeaseKey, matchedLease, matchedLeaseKey != nil, nil
}
