package crawlbroker

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func setScriptedSequence(
	t *testing.T,
	fixture scriptedQueueFixture,
	key vault.Key,
	value uint64,
) {
	t.Helper()
	raw, err := (sequenceCodec{}).Encode(value)
	if err != nil {
		t.Fatalf("encode sequence: %v", err)
	}
	fixture.engine.buckets[seqBucket][string(key)] = raw
}

func setScriptedOrder(
	t *testing.T,
	fixture scriptedQueueFixture,
	sequence uint64,
	order yagocrawlcontract.CrawlOrder,
) {
	t.Helper()
	data, err := yagocrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal order: %v", err)
	}
	fixture.engine.buckets[orderBucket][string(orderKey(sequence))] = data
}

func prepareScriptedPriorityTail(
	t *testing.T,
	fixture scriptedQueueFixture,
	order yagocrawlcontract.CrawlOrder,
) {
	t.Helper()
	setScriptedSequence(t, fixture, priorityIndexFormatKey, priorityIndexFormatVersion)
	setScriptedSequence(t, fixture, priorityIndexNextKey, 0)
	setScriptedSequence(t, fixture, seqKey, 1)
	setScriptedOrder(t, fixture, 0, order)
}

func TestPriorityReconciliationSurfacesPersistentStateErrors(t *testing.T) {
	t.Run("transaction", func(t *testing.T) {
		fixture := scriptedQueue(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := fixture.queue.reconcilePriorityIndexes(ctx); err == nil {
			t.Fatal("expected cancelled reconciliation to fail")
		}
	})

	t.Run("format decode", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.buckets[seqBucket][string(priorityIndexFormatKey)] = []byte{1}
		if err := fixture.queue.reconcilePriorityIndexes(t.Context()); err == nil {
			t.Fatal("expected priority format decode failure")
		}
	})

	t.Run("future format", func(t *testing.T) {
		fixture := scriptedQueue(t)
		setScriptedSequence(t, fixture, priorityIndexFormatKey, 2)
		if err := fixture.queue.reconcilePriorityIndexes(t.Context()); err == nil {
			t.Fatal("expected future priority format rejection")
		}
	})

	t.Run("watermark decode", func(t *testing.T) {
		fixture := scriptedQueue(t)
		setScriptedSequence(t, fixture, priorityIndexFormatKey, priorityIndexFormatVersion)
		fixture.engine.buckets[seqBucket][string(priorityIndexNextKey)] = []byte{1}
		if err := fixture.queue.reconcilePriorityIndexes(t.Context()); err == nil {
			t.Fatal("expected priority watermark decode failure")
		}
	})

	t.Run("legacy scan", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.scanErrors[automaticOrderIndexBucket] = errors.New("scan failed")
		if err := fixture.queue.reconcilePriorityIndexes(t.Context()); err == nil {
			t.Fatal("expected legacy automatic scan failure")
		}
	})

	t.Run("sequence decode", func(t *testing.T) {
		fixture := scriptedQueue(t)
		setScriptedSequence(t, fixture, priorityIndexFormatKey, priorityIndexFormatVersion)
		setScriptedSequence(t, fixture, priorityIndexNextKey, 0)
		fixture.engine.buckets[seqBucket][string(seqKey)] = []byte{1}
		if err := fixture.queue.reconcilePriorityIndexes(t.Context()); err == nil {
			t.Fatal("expected order sequence decode failure")
		}
	})

	t.Run("watermark ahead", func(t *testing.T) {
		fixture := scriptedQueue(t)
		setScriptedSequence(t, fixture, priorityIndexFormatKey, priorityIndexFormatVersion)
		setScriptedSequence(t, fixture, priorityIndexNextKey, 2)
		setScriptedSequence(t, fixture, seqKey, 1)
		if err := fixture.queue.reconcilePriorityIndexes(t.Context()); err == nil {
			t.Fatal("expected priority watermark range failure")
		}
	})
}

func TestPriorityReconciliationSurfacesPersistentScanAndStoreErrors(t *testing.T) {
	t.Run("pending scan", func(t *testing.T) {
		fixture := scriptedQueue(t)
		prepareScriptedPriorityTail(t, fixture, testOrder("normal"))
		fixture.engine.scanErrors[orderBucket] = errors.New("scan failed")
		if err := fixture.queue.reconcilePriorityIndexes(t.Context()); err == nil {
			t.Fatal("expected pending order scan failure")
		}
	})

	t.Run("watermark store", func(t *testing.T) {
		fixture := scriptedQueue(t)
		prepareScriptedPriorityTail(t, fixture, testOrder("normal"))
		fixture.engine.putKeyErrors[seqBucket] = map[string]error{
			string(priorityIndexNextKey): errors.New("put failed"),
		}
		if err := fixture.queue.reconcilePriorityIndexes(t.Context()); err == nil {
			t.Fatal("expected priority watermark store failure")
		}
	})

	t.Run("format store", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.putKeyErrors[seqBucket] = map[string]error{
			string(priorityIndexFormatKey): errors.New("put failed"),
		}
		if err := fixture.queue.reconcilePriorityIndexes(t.Context()); err == nil {
			t.Fatal("expected priority format store failure")
		}
	})
}

func TestPriorityReconciliationSurfacesIndexMutationErrors(t *testing.T) {
	t.Run("migrate payload", func(t *testing.T) {
		fixture := scriptedQueue(t)
		setScriptedOrder(t, fixture, 0, automaticOrder("legacy"))
		data := fixture.engine.buckets[orderBucket][string(orderKey(0))]
		delete(fixture.engine.buckets[orderBucket], string(orderKey(0)))
		fixture.engine.buckets[automaticOrderIndexBucket][string(orderKey(0))] = data
		fixture.engine.putErrors[orderBucket] = errors.New("put failed")
		if err := fixture.queue.reconcilePriorityIndexes(t.Context()); err == nil {
			t.Fatal("expected legacy payload migration failure")
		}
	})

	t.Run("replace legacy payload", func(t *testing.T) {
		fixture := scriptedQueue(t)
		setScriptedOrder(t, fixture, 0, automaticOrder("legacy"))
		data := fixture.engine.buckets[orderBucket][string(orderKey(0))]
		fixture.engine.buckets[automaticOrderIndexBucket][string(orderKey(0))] = data
		fixture.engine.putKeyErrors[automaticOrderIndexBucket] = map[string]error{
			string(orderKey(0)): errors.New("put failed"),
		}
		if err := fixture.queue.reconcilePriorityIndexes(t.Context()); err == nil {
			t.Fatal("expected legacy payload replacement failure")
		}
	})

	t.Run("index priority", func(t *testing.T) {
		fixture := scriptedQueue(t)
		prepareScriptedPriorityTail(t, fixture, testOrder("normal"))
		fixture.engine.putErrors[normalOrderIndexBucket] = errors.New("put failed")
		if err := fixture.queue.reconcilePriorityIndexes(t.Context()); err == nil {
			t.Fatal("expected priority index store failure")
		}
	})

	t.Run("remove obsolete priority", func(t *testing.T) {
		fixture := scriptedQueue(t)
		prepareScriptedPriorityTail(t, fixture, automaticOrder("automatic"))
		fixture.engine.buckets[normalOrderIndexBucket][string(orderKey(0))] = priorityIndexMarker
		fixture.engine.deleteErrors[normalOrderIndexBucket] = errors.New("delete failed")
		if err := fixture.queue.reconcilePriorityIndexes(t.Context()); err == nil {
			t.Fatal("expected obsolete priority deletion failure")
		}
	})
}

func TestPrioritySelectionSurfacesStaleIndexDeletionErrors(t *testing.T) {
	for _, bucket := range []vault.Name{normalOrderIndexBucket, automaticOrderIndexBucket} {
		t.Run(string(bucket), func(t *testing.T) {
			fixture := scriptedQueue(t)
			fixture.engine.buckets[bucket][string(orderKey(0))] = priorityIndexMarker
			fixture.engine.deleteErrors[bucket] = errors.New("delete failed")
			if _, _, _, err := fixture.queue.leasePop(t.Context(), "worker"); err == nil {
				t.Fatal("expected stale priority marker deletion failure")
			}
		})
	}
}
