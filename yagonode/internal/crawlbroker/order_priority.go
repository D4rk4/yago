package crawlbroker

import (
	"bytes"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

var priorityBurstKey = vault.Key("automaticDiscoveryBurst")

type pendingOrderHead struct {
	index     *vault.Collection[[]byte]
	key       vault.Key
	data      []byte
	found     bool
	automatic bool
}

func (q *DurableOrderQueue) SetAutomaticDiscoveryPriority(enabled bool) {
	q.prioritizeAutomatic.Store(enabled)
}

func (q *DurableOrderQueue) priorityIndex(
	priority yagocrawlcontract.CrawlOrderPriority,
) *vault.Collection[[]byte] {
	if priority == yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery {
		return q.automaticOrderIndex
	}

	return q.normalOrderIndex
}

func (q *DurableOrderQueue) pendingHead(
	tx *vault.Txn,
	index *vault.Collection[[]byte],
	automatic bool,
) (pendingOrderHead, []vault.Key, error) {
	head := pendingOrderHead{index: index, automatic: automatic}
	var stale []vault.Key
	if err := index.Scan(tx, nil, func(key vault.Key, _ []byte) (bool, error) {
		data, found := tx.ReadBucketValue(orderBucket, key)
		if !found {
			stale = append(stale, append(vault.Key(nil), key...))

			return true, nil
		}
		head.key = append(vault.Key(nil), key...)
		head.data = append([]byte(nil), data...)
		head.found = true

		return false, nil
	}); err != nil {
		return pendingOrderHead{}, nil, fmt.Errorf("scan crawl order priority index: %w", err)
	}

	return head, stale, nil
}

func prunePriorityIndexEntries(
	tx *vault.Txn,
	index *vault.Collection[[]byte],
	keys []vault.Key,
) error {
	for _, key := range keys {
		if _, err := index.Delete(tx, key); err != nil {
			return fmt.Errorf("delete stale crawl order priority: %w", err)
		}
	}

	return nil
}

func (q *DurableOrderQueue) selectPendingOrder(
	tx *vault.Txn,
) (pendingOrderHead, uint64, bool, error) {
	normal, staleNormal, err := q.pendingHead(tx, q.normalOrderIndex, false)
	if err != nil {
		return pendingOrderHead{}, 0, false, err
	}
	automatic, staleAutomatic, err := q.pendingHead(tx, q.automaticOrderIndex, true)
	if err != nil {
		return pendingOrderHead{}, 0, false, err
	}
	if err := prunePriorityIndexEntries(tx, q.normalOrderIndex, staleNormal); err != nil {
		return pendingOrderHead{}, 0, false, err
	}
	if err := prunePriorityIndexEntries(tx, q.automaticOrderIndex, staleAutomatic); err != nil {
		return pendingOrderHead{}, 0, false, err
	}
	burst, _, err := q.seq.Get(tx, priorityBurstKey)
	if err != nil {
		return pendingOrderHead{}, 0, false, fmt.Errorf("read automatic crawl burst: %w", err)
	}

	if !normal.found && !automatic.found {
		return pendingOrderHead{}, burst, false, nil
	}
	if !q.prioritizeAutomatic.Load() {
		switch {
		case !automatic.found:
			return normal, 0, burst != 0, nil
		case !normal.found:
			return automatic, 0, burst != 0, nil
		case bytes.Compare(normal.key, automatic.key) <= 0:
			return normal, 0, burst != 0, nil
		default:
			return automatic, 0, burst != 0, nil
		}
	}

	preferAutomatic := automatic.found &&
		(!normal.found || burst < yagocrawlcontract.AutomaticDiscoveryPriorityBurst)
	if preferAutomatic {
		if !normal.found {
			return automatic, 0, burst != 0, nil
		}

		return automatic, burst + 1, true, nil
	}

	return normal, 0, burst != 0, nil
}

func orderPriority(automatic bool) yagocrawlcontract.CrawlOrderPriority {
	if automatic {
		return yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery
	}

	return yagocrawlcontract.CrawlOrderPriorityNormal
}

func persistedOrderPriority(
	data []byte,
	priority yagocrawlcontract.CrawlOrderPriority,
) yagocrawlcontract.CrawlOrderPriority {
	if priority == yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery {
		return priority
	}
	order, err := yagocrawlcontract.UnmarshalCrawlOrder(data)
	if err == nil && order.Priority == yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery {
		return yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery
	}

	return yagocrawlcontract.CrawlOrderPriorityNormal
}
