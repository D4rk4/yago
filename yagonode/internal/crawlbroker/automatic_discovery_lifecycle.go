package crawlbroker

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (q *DurableOrderQueue) activeAutomaticDiscoveryForOrderTx(
	tx *vault.Txn,
	orderData []byte,
	sequence uint64,
) (string, bool, error) {
	order, err := yagocrawlcontract.UnmarshalCrawlOrder(orderData)
	if err != nil {
		return "", false, fmt.Errorf("decode pending crawl discovery: %w", err)
	}
	if order.Priority != yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery {
		return "", false, nil
	}
	for _, request := range order.Requests {
		activeSequence, found, err := q.activeDiscoveryKeys.Get(
			tx,
			vault.Key(request.URL),
		)
		if err != nil {
			return "", false, fmt.Errorf("read pending crawl discovery: %w", err)
		}
		if found && activeSequence == sequence {
			return request.URL, true, nil
		}
	}

	return "", false, nil
}

func (q *DurableOrderQueue) recordRequeuedAutomaticDiscoveryTx(
	tx *vault.Txn,
	leaseID string,
	record leaseRecord,
	sequence uint64,
) error {
	if record.DiscoveryKey == "" {
		return nil
	}
	if err := q.releaseLeasedAutomaticDiscoveryTx(tx, leaseID, record); err != nil {
		return err
	}
	if err := q.activeDiscoveryKeys.Put(
		tx,
		vault.Key(record.DiscoveryKey),
		sequence,
	); err != nil {
		return fmt.Errorf("record requeued crawl discovery: %w", err)
	}
	if err := q.pendingDiscoveryKeys.Put(
		tx,
		orderKey(sequence),
		[]byte(record.DiscoveryKey),
	); err != nil {
		return fmt.Errorf("record pending requeued crawl discovery: %w", err)
	}

	return nil
}

func (q *DurableOrderQueue) releaseLeasedAutomaticDiscoveryTx(
	tx *vault.Txn,
	leaseID string,
	record leaseRecord,
) error {
	if record.DiscoveryKey == "" {
		return nil
	}
	indexedLeaseID, found, err := q.leasedDiscoveryKeys.Get(
		tx,
		vault.Key(record.DiscoveryKey),
	)
	if err != nil {
		return fmt.Errorf("read leased crawl discovery: %w", err)
	}
	if !found || string(indexedLeaseID) != leaseID {
		return nil
	}
	if _, err := q.leasedDiscoveryKeys.Delete(
		tx,
		vault.Key(record.DiscoveryKey),
	); err != nil {
		return fmt.Errorf("release leased crawl discovery: %w", err)
	}

	return nil
}

func (q *DurableOrderQueue) recordLeasedAutomaticDiscoveryTx(
	tx *vault.Txn,
	discoveryKey string,
	leaseKey vault.Key,
) error {
	indexedLeaseID, found, err := q.leasedDiscoveryKeys.Get(
		tx,
		vault.Key(discoveryKey),
	)
	if err != nil {
		return fmt.Errorf("read leased crawl discovery key: %w", err)
	}
	if found && string(indexedLeaseID) == string(leaseKey) {
		return nil
	}
	if err := q.leasedDiscoveryKeys.Put(
		tx,
		vault.Key(discoveryKey),
		leaseKey,
	); err != nil {
		return fmt.Errorf("record leased crawl discovery key: %w", err)
	}

	return nil
}

func (q *DurableOrderQueue) releaseActiveAutomaticDiscoveryTx(
	tx *vault.Txn,
	record leaseRecord,
) error {
	if record.DiscoveryKey == "" {
		return nil
	}
	sequence, found, err := q.activeDiscoveryKeys.Get(
		tx,
		vault.Key(record.DiscoveryKey),
	)
	if err != nil {
		return fmt.Errorf("read terminal crawl discovery: %w", err)
	}
	if !found || sequence != record.DiscoverySequence {
		return nil
	}
	if _, err := q.activeDiscoveryKeys.Delete(
		tx,
		vault.Key(record.DiscoveryKey),
	); err != nil {
		return fmt.Errorf("release terminal crawl discovery: %w", err)
	}

	return nil
}

func (q *DurableOrderQueue) releaseActiveAutomaticDiscovery(
	ctx context.Context,
	record leaseRecord,
) error {
	if record.DiscoveryKey == "" {
		return nil
	}
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		return q.releaseActiveAutomaticDiscoveryTx(tx, record)
	}); err != nil {
		return fmt.Errorf("release active crawl discovery: %w", err)
	}

	return nil
}
