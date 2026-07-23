package crawlbroker

import (
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (q *DurableOrderQueue) discardPendingAutomaticDiscoveryLeaseDuplicateTx(
	tx *vault.Txn,
	order pendingOrderHead,
) (bool, error) {
	if !order.automatic {
		return false, nil
	}
	sequence, err := (sequenceCodec{}).Decode(order.key)
	if err != nil {
		return false, fmt.Errorf("decode pending crawl discovery sequence: %w", err)
	}
	discoveryKey, found, err := q.pendingDiscoveryKeys.Get(tx, order.key)
	if err != nil {
		return false, fmt.Errorf("read pending crawl discovery: %w", err)
	}
	if !found {
		key, active, err := q.activeAutomaticDiscoveryForOrderTx(
			tx,
			order.data,
			sequence,
		)
		if err != nil {
			return false, err
		}
		if !active {
			return false, nil
		}
		discoveryKey = []byte(key)
	}
	leaseKey, lease, leased, err := q.automaticDiscoveryLeaseTx(
		tx,
		string(discoveryKey),
	)
	if err != nil || !leased {
		return false, err
	}
	if _, err := q.orders.Delete(tx, order.key); err != nil {
		return false, fmt.Errorf("delete duplicate crawl discovery: %w", err)
	}
	if _, err := order.index.Delete(tx, order.key); err != nil {
		return false, fmt.Errorf("delete duplicate crawl discovery priority: %w", err)
	}
	if _, err := q.pendingDiscoveryKeys.Delete(tx, order.key); err != nil {
		return false, fmt.Errorf("delete duplicate pending crawl discovery: %w", err)
	}
	lease.DiscoveryKey = string(discoveryKey)
	if err := q.leases.Put(tx, leaseKey, lease); err != nil {
		return false, fmt.Errorf("repair crawl discovery lease: %w", err)
	}
	if err := q.recordLeasedAutomaticDiscoveryTx(
		tx,
		string(discoveryKey),
		leaseKey,
	); err != nil {
		return false, err
	}
	if err := q.activeDiscoveryKeys.Put(
		tx,
		vault.Key(discoveryKey),
		lease.DiscoverySequence,
	); err != nil {
		return false, fmt.Errorf("repair active crawl discovery lease: %w", err)
	}

	return true, nil
}
