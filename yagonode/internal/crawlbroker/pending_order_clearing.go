package crawlbroker

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const maximumPendingOrdersPerClearTransaction = 256

type pendingOrderClearPage struct {
	removed      int
	lastOrderKey vault.Key
	finished     bool
}

func (q *DurableOrderQueue) ClearPendingOrders(ctx context.Context) (int, error) {
	q.discoveryAdmission.Lock()
	defer q.discoveryAdmission.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("clear pending crawl orders: %w", err)
	}
	if err := q.reconcileAutomaticDiscoveryIntents(ctx); err != nil {
		return 0, fmt.Errorf("reconcile crawl discovery before clear: %w", err)
	}
	next, err := q.pendingOrderSequenceCutoff(ctx)
	if err != nil {
		return 0, err
	}
	removed, err := q.clearPendingOrdersBefore(ctx, next)
	if removed > 0 {
		q.signal()
	}

	return removed, err
}

func (q *DurableOrderQueue) pendingOrderSequenceCutoff(ctx context.Context) (uint64, error) {
	var nextSequence uint64
	err := q.vault.View(ctx, func(tx *vault.Txn) error {
		sequence, _, err := q.seq.Get(tx, seqKey)
		if err != nil {
			return fmt.Errorf("read crawl order sequence: %w", err)
		}
		lastKey, err := tx.ReadBucketLastKey(orderBucket)
		if err != nil {
			return fmt.Errorf("read last pending crawl order: %w", err)
		}
		if len(lastKey) != 0 {
			lastSequence, err := (sequenceCodec{}).Decode(lastKey)
			if err != nil {
				return fmt.Errorf("decode last pending crawl order sequence: %w", err)
			}
			if lastSequence >= sequence {
				return fmt.Errorf(
					"crawl order sequence %d precedes pending order %d",
					sequence,
					lastSequence,
				)
			}
		}
		nextSequence = sequence

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("capture pending crawl order cutoff: %w", err)
	}

	return nextSequence, nil
}

func (q *DurableOrderQueue) clearPendingOrdersBefore(
	ctx context.Context,
	nextSequence uint64,
) (int, error) {
	removed := 0
	var lastProcessedOrderKey vault.Key
	for {
		if err := ctx.Err(); err != nil {
			return removed, fmt.Errorf("clear pending crawl orders: %w", err)
		}
		page, err := q.clearPendingOrderPage(ctx, lastProcessedOrderKey, nextSequence)
		if err != nil {
			return removed, err
		}
		removed += page.removed
		lastProcessedOrderKey = page.lastOrderKey
		if page.finished {
			return removed, nil
		}
	}
}

func (q *DurableOrderQueue) clearPendingOrderPage(
	ctx context.Context,
	after vault.Key,
	nextSequence uint64,
) (pendingOrderClearPage, error) {
	var page pendingOrderClearPage
	err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		var err error
		page, err = q.clearPendingOrderPageTx(tx, after, nextSequence)

		return err
	})
	if err != nil {
		return pendingOrderClearPage{}, fmt.Errorf("clear pending crawl order page: %w", err)
	}

	return page, nil
}

func (q *DurableOrderQueue) clearPendingOrderPageTx(
	tx *vault.Txn,
	after vault.Key,
	nextSequence uint64,
) (pendingOrderClearPage, error) {
	rows, err := tx.ReadBucketPage(orderBucket, after, maximumPendingOrdersPerClearTransaction)
	if err != nil {
		return pendingOrderClearPage{}, fmt.Errorf("read pending crawl order page: %w", err)
	}
	if len(rows.Entries) == 0 {
		return pendingOrderClearPage{finished: true}, nil
	}

	page := pendingOrderClearPage{finished: !rows.More}
	for _, entry := range rows.Entries {
		sequence, err := (sequenceCodec{}).Decode(entry.Key)
		if err != nil {
			return pendingOrderClearPage{}, fmt.Errorf(
				"decode pending crawl order sequence: %w",
				err,
			)
		}
		if sequence >= nextSequence {
			page.finished = true

			break
		}
		if err := q.deletePendingOrderEntryTx(tx, entry.Key, sequence, entry.Value); err != nil {
			return pendingOrderClearPage{}, err
		}
		page.removed++
		page.lastOrderKey = append(page.lastOrderKey[:0], entry.Key...)
	}
	return page, nil
}

func (q *DurableOrderQueue) deletePendingOrderEntryTx(
	tx *vault.Txn,
	orderSequenceKey vault.Key,
	sequence uint64,
	data []byte,
) error {
	order, err := yagocrawlcontract.UnmarshalCrawlOrder(data)
	if err != nil {
		return fmt.Errorf("decode pending crawl order: %w", err)
	}

	return q.deletePendingOrderTx(tx, orderSequenceKey, sequence, order)
}

func (q *DurableOrderQueue) deletePendingOrderTx(
	tx *vault.Txn,
	sequenceKey vault.Key,
	sequence uint64,
	order yagocrawlcontract.CrawlOrder,
) error {
	if order.Priority == yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery {
		if err := q.deletePendingAutomaticDiscoveryIdentityTx(
			tx,
			sequenceKey,
			sequence,
			order,
		); err != nil {
			return err
		}
	}
	_, err := q.orders.Delete(tx, sequenceKey)
	if err != nil {
		return fmt.Errorf("delete pending crawl order: %w", err)
	}
	if _, err := q.normalOrderIndex.Delete(tx, sequenceKey); err != nil {
		return fmt.Errorf("delete normal crawl order index: %w", err)
	}
	if _, err := q.automaticOrderIndex.Delete(tx, sequenceKey); err != nil {
		return fmt.Errorf("delete automatic crawl order index: %w", err)
	}

	return nil
}

func (q *DurableOrderQueue) deletePendingAutomaticDiscoveryIdentityTx(
	tx *vault.Txn,
	sequenceKey vault.Key,
	sequence uint64,
	order yagocrawlcontract.CrawlOrder,
) error {
	indexedKey, indexed, err := q.pendingDiscoveryKeys.Get(tx, sequenceKey)
	if err != nil {
		return fmt.Errorf("read pending crawl discovery identity: %w", err)
	}
	if indexed {
		if len(indexedKey) == 0 {
			return fmt.Errorf("pending crawl discovery identity is empty")
		}
		if _, err := q.pendingDiscoveryKeys.Delete(tx, sequenceKey); err != nil {
			return fmt.Errorf("delete pending crawl discovery identity: %w", err)
		}

		return q.deletePendingAutomaticDiscoveryKeyTx(tx, string(indexedKey), sequence)
	}

	for _, request := range order.Requests {
		if err := q.deletePendingAutomaticDiscoveryKeyTx(tx, request.URL, sequence); err != nil {
			return err
		}
	}

	return nil
}

func (q *DurableOrderQueue) deletePendingAutomaticDiscoveryKeyTx(
	tx *vault.Txn,
	key string,
	sequence uint64,
) error {
	_, leased, err := q.leasedDiscoveryKeys.Get(tx, vault.Key(key))
	if err != nil {
		return fmt.Errorf("read leased crawl discovery identity: %w", err)
	}
	activeSequence, active, err := q.activeDiscoveryKeys.Get(tx, vault.Key(key))
	if err != nil {
		return fmt.Errorf("read active crawl discovery identity: %w", err)
	}
	legacySequence, legacy, err := q.keys.Get(tx, vault.Key(key))
	if err != nil {
		return fmt.Errorf("read legacy crawl discovery identity: %w", err)
	}
	if !leased && active && activeSequence == sequence {
		if _, err := q.activeDiscoveryKeys.Delete(tx, vault.Key(key)); err != nil {
			return fmt.Errorf("delete active crawl discovery identity: %w", err)
		}
	}
	if !leased && legacy && legacySequence == sequence {
		if _, err := q.keys.Delete(tx, vault.Key(key)); err != nil {
			return fmt.Errorf("delete legacy crawl discovery identity: %w", err)
		}
	}

	return nil
}
