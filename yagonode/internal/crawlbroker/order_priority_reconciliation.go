package crawlbroker

import (
	"bytes"
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type automaticOrderPayload struct {
	key  vault.Key
	data []byte
}

func (q *DurableOrderQueue) reconcilePriorityIndexes(ctx context.Context) error {
	if err := q.vault.Update(ctx, q.reconcilePriorityIndexesTx); err != nil {
		return fmt.Errorf("reconcile crawl order priority indexes: %w", err)
	}

	return nil
}

func (q *DurableOrderQueue) reconcilePriorityIndexesTx(tx *vault.Txn) error {
	format, formatFound, err := q.seq.Get(tx, priorityIndexFormatKey)
	if err != nil {
		return fmt.Errorf("read crawl order priority format: %w", err)
	}
	if formatFound && format != priorityIndexFormatVersion {
		return fmt.Errorf("unsupported crawl order priority format %d", format)
	}
	indexedThrough, indexedThroughFound, err := q.seq.Get(tx, priorityIndexNextKey)
	if err != nil {
		return fmt.Errorf("read crawl order priority watermark: %w", err)
	}
	if !formatFound {
		if err := q.migrateAutomaticOrderPayloads(tx); err != nil {
			return err
		}
		indexedThrough = 0
		indexedThroughFound = false
	}
	next, _, err := q.seq.Get(tx, seqKey)
	if err != nil {
		return fmt.Errorf("read crawl order sequence for priority reconciliation: %w", err)
	}
	if indexedThrough > next {
		return fmt.Errorf(
			"crawl order priority watermark %d exceeds sequence %d",
			indexedThrough,
			next,
		)
	}
	if !indexedThroughFound || indexedThrough < next {
		if err := q.indexPendingOrders(tx, indexedThrough, !indexedThroughFound); err != nil {
			return err
		}
		if err := q.seq.Put(tx, priorityIndexNextKey, next); err != nil {
			return fmt.Errorf("store crawl order priority watermark: %w", err)
		}
	}
	if !formatFound {
		if err := q.seq.Put(tx, priorityIndexFormatKey, priorityIndexFormatVersion); err != nil {
			return fmt.Errorf("store crawl order priority format: %w", err)
		}
	}

	return nil
}

func (q *DurableOrderQueue) migrateAutomaticOrderPayloads(tx *vault.Txn) error {
	var payloads []automaticOrderPayload
	if err := q.automaticOrderIndex.Scan(tx, nil, func(key vault.Key, data []byte) (bool, error) {
		if !bytes.Equal(data, priorityIndexMarker) {
			payloads = append(payloads, automaticOrderPayload{
				key:  append(vault.Key(nil), key...),
				data: append([]byte(nil), data...),
			})
		}

		return true, nil
	}); err != nil {
		return fmt.Errorf("scan legacy automatic crawl orders: %w", err)
	}
	for _, payload := range payloads {
		_, found := tx.ReadBucketValue(orderBucket, payload.key)
		if !found {
			if err := q.orders.Put(tx, payload.key, payload.data); err != nil {
				return fmt.Errorf("migrate automatic crawl order: %w", err)
			}
		}
		if err := q.automaticOrderIndex.Put(tx, payload.key, priorityIndexMarker); err != nil {
			return fmt.Errorf("replace automatic crawl order payload: %w", err)
		}
	}

	return nil
}

func (q *DurableOrderQueue) indexPendingOrders(
	tx *vault.Txn,
	indexedThrough uint64,
	indexAll bool,
) error {
	firstUnindexedKey := orderKey(indexedThrough)
	if err := q.orders.Scan(tx, nil, func(key vault.Key, data []byte) (bool, error) {
		if !indexAll && bytes.Compare(key, firstUnindexedKey) < 0 {
			return true, nil
		}
		priority := persistedOrderPriority(data, yagocrawlcontract.CrawlOrderPriorityNormal)
		target := q.priorityIndex(priority)
		other := q.normalOrderIndex
		if priority == yagocrawlcontract.CrawlOrderPriorityNormal {
			other = q.automaticOrderIndex
		}
		if err := target.Put(tx, key, priorityIndexMarker); err != nil {
			return false, fmt.Errorf("index crawl order priority: %w", err)
		}
		if _, err := other.Delete(tx, key); err != nil {
			return false, fmt.Errorf("remove obsolete crawl order priority: %w", err)
		}

		return true, nil
	}); err != nil {
		return fmt.Errorf("scan pending crawl orders for priority: %w", err)
	}

	return nil
}
