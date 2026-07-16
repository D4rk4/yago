package crawlbroker

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func publishWithoutPriorityIndex(
	t *testing.T,
	queue *DurableOrderQueue,
	order yagocrawlcontract.CrawlOrder,
) {
	t.Helper()
	data, err := yagocrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal legacy order: %v", err)
	}
	var key vault.Key
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		next, _, err := queue.seq.Get(tx, seqKey)
		if err != nil {
			return fmt.Errorf("read legacy sequence: %w", err)
		}
		key = orderKey(next)
		if err := queue.orders.Put(tx, key, data); err != nil {
			return fmt.Errorf("store legacy order: %w", err)
		}

		if err := queue.seq.Put(tx, seqKey, next+1); err != nil {
			return fmt.Errorf("advance legacy sequence: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("publish legacy order: %v", err)
	}
}

func TestPriorityQueueKeepsEveryPayloadInLegacyCanonicalBucket(t *testing.T) {
	queue := memQueue(t)
	publishOrders(t, queue, testOrder("normal"), automaticOrder("automatic"))

	var names []string
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		return queue.orders.Scan(tx, nil, func(_ vault.Key, data []byte) (bool, error) {
			order, err := yagocrawlcontract.UnmarshalCrawlOrder(data)
			if err != nil {
				return false, fmt.Errorf("decode canonical order: %w", err)
			}
			names = append(names, order.Profile.Name)

			return true, nil
		})
	}); err != nil {
		t.Fatalf("scan canonical orders: %v", err)
	}
	if want := []string{"normal", "automatic"}; !equalNames(names, want) {
		t.Fatalf("canonical orders = %v, want %v", names, want)
	}
}

func TestPriorityReconciliationIndexesOrdersAddedByOlderNode(t *testing.T) {
	queue := memQueue(t)
	if err := queue.reconcilePriorityIndexes(t.Context()); err != nil {
		t.Fatalf("initialize priority indexes: %v", err)
	}
	publishOrders(t, queue, testOrder("normal"))
	publishWithoutPriorityIndex(t, queue, automaticOrder("automatic-from-old-node"))

	if err := queue.reconcilePriorityIndexes(t.Context()); err != nil {
		t.Fatalf("reconcile legacy tail: %v", err)
	}
	for _, want := range []string{"automatic-from-old-node", "normal"} {
		if got := leaseOrderName(t, queue); got != want {
			t.Fatalf("leased %q, want %q", got, want)
		}
	}
}

func TestPrioritySelectionPrunesOrdersConsumedByOlderNode(t *testing.T) {
	queue := memQueue(t)
	if err := queue.reconcilePriorityIndexes(t.Context()); err != nil {
		t.Fatalf("initialize priority indexes: %v", err)
	}
	publishOrders(t, queue, automaticOrder("already-consumed"))
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		_, err := queue.orders.Delete(tx, orderKey(0))
		if err != nil {
			return fmt.Errorf("delete canonical order: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("simulate old-node consumption: %v", err)
	}
	publishOrders(t, queue, testOrder("normal"))
	if got := leaseOrderName(t, queue); got != "normal" {
		t.Fatalf("leased %q, want normal", got)
	}
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		if queue.automaticOrderIndex.Contains(tx, orderKey(0)) {
			t.Fatal("stale automatic priority marker survived selection")
		}

		return nil
	}); err != nil {
		t.Fatalf("inspect automatic priority index: %v", err)
	}
}

func TestPriorityReconciliationMigratesSplitAutomaticPayloads(t *testing.T) {
	queue := memQueue(t)
	data, err := yagocrawlcontract.MarshalCrawlOrder(automaticOrder("split-payload"))
	if err != nil {
		t.Fatalf("marshal automatic order: %v", err)
	}
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := queue.automaticOrderIndex.Put(tx, orderKey(0), data); err != nil {
			return fmt.Errorf("store split automatic payload: %w", err)
		}

		if err := queue.seq.Put(tx, seqKey, 1); err != nil {
			return fmt.Errorf("advance split payload sequence: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("prepare split payload: %v", err)
	}

	if err := queue.reconcilePriorityIndexes(t.Context()); err != nil {
		t.Fatalf("migrate split payload: %v", err)
	}
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		canonical, found := tx.ReadBucketValue(orderBucket, orderKey(0))
		if !found || !bytes.Equal(canonical, data) {
			t.Fatalf("canonical payload = %q/%v, want migrated payload", canonical, found)
		}
		marker, found := tx.ReadBucketValue(automaticOrderIndexBucket, orderKey(0))
		if !found || !bytes.Equal(marker, priorityIndexMarker) {
			t.Fatalf("automatic marker = %v/%v", marker, found)
		}

		return nil
	}); err != nil {
		t.Fatalf("inspect migrated payload: %v", err)
	}
	publishOrders(t, queue, testOrder("normal"))
	if got := leaseOrderName(t, queue); got != "split-payload" {
		t.Fatalf("leased %q, want migrated automatic order", got)
	}
}

func TestPriorityReconciliationTreatsUnknownAndMalformedOrdersAsNormal(t *testing.T) {
	queue := memQueue(t)
	if err := queue.reconcilePriorityIndexes(t.Context()); err != nil {
		t.Fatalf("initialize priority indexes: %v", err)
	}
	unknown := testOrder("unknown")
	unknown.Priority = yagocrawlcontract.CrawlOrderPriority("future")
	publishWithoutPriorityIndex(t, queue, unknown)
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		next, _, err := queue.seq.Get(tx, seqKey)
		if err != nil {
			return fmt.Errorf("read malformed order sequence: %w", err)
		}
		if err := queue.orders.Put(tx, orderKey(next), []byte("malformed")); err != nil {
			return fmt.Errorf("store malformed order: %w", err)
		}

		if err := queue.seq.Put(tx, seqKey, next+1); err != nil {
			return fmt.Errorf("advance malformed order sequence: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("publish malformed legacy order: %v", err)
	}
	if err := queue.reconcilePriorityIndexes(t.Context()); err != nil {
		t.Fatalf("reconcile conservative priorities: %v", err)
	}
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		for sequence := range uint64(2) {
			if !queue.normalOrderIndex.Contains(tx, orderKey(sequence)) {
				t.Fatalf("order %d was not indexed as normal", sequence)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("inspect normal priority index: %v", err)
	}
}

func equalNames(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}

	return true
}
