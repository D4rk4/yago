package peernews

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestNewsCleanupCodecAndCursorFailures(t *testing.T) {
	assertNewsCleanupCodecFailures(t)
	if isStaleNewsCleanupCursor(errors.New("different")) {
		t.Fatal("unrelated error classified as stale cursor")
	}
	failure := errors.New("cleanup cursor storage failed")
	for _, test := range []struct {
		name string
		run  func(*testing.T, error)
	}{
		{name: "read cancellation", run: assertNewsCleanupReadCancellation},
		{name: "discard corrupt", run: assertCorruptNewsCleanupDiscarded},
		{name: "discard corrupt failure", run: assertCorruptNewsCleanupDiscardFailure},
		{name: "store failure", run: assertNewsCleanupStoreFailure},
		{name: "clear failure", run: assertNewsCleanupClearFailure},
		{name: "clear sequence failure", run: assertNewsCleanupSequenceClearFailure},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.run(t, failure)
		})
	}
}

func assertNewsCleanupCodecFailures(t *testing.T) {
	t.Helper()
	codec := newsCleanupCodec{}
	for _, value := range []string{"", strings.Repeat("x", maximumNewsCleanupValueBytes+1)} {
		if _, err := codec.Encode(value); err == nil {
			t.Fatalf("Encode(%d bytes) succeeded", len(value))
		}
		if _, err := codec.Decode([]byte(value)); err == nil {
			t.Fatalf("Decode(%d bytes) succeeded", len(value))
		}
	}
}

func assertNewsCleanupReadCancellation(t *testing.T, _ error) {
	t.Helper()
	pool := openStubPool(t, newNewsStubEngine())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := pool.cleanupCursor(ctx, knownCleanupCursorKey); !errors.Is(err, context.Canceled) {
		t.Fatalf("cursor error = %v", err)
	}
}

func assertCorruptNewsCleanupDiscarded(t *testing.T, _ error) {
	t.Helper()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	engine.buckets[cleanupBucket][string(knownCleanupCursorKey)] = []byte(
		strings.Repeat("x", maximumNewsCleanupValueBytes+1),
	)
	cursor, err := pool.cleanupCursor(t.Context(), knownCleanupCursorKey)
	if err != nil || cursor != nil || len(engine.buckets[cleanupBucket]) != 0 {
		t.Fatalf("corrupt cursor = %q/%v", cursor, err)
	}
}

func assertCorruptNewsCleanupDiscardFailure(t *testing.T, failure error) {
	t.Helper()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	engine.buckets[cleanupBucket][string(knownCleanupCursorKey)] = []byte(
		strings.Repeat("x", maximumNewsCleanupValueBytes+1),
	)
	engine.deleteErrors[cleanupBucket] = failure
	if _, err := pool.cleanupCursor(
		t.Context(), knownCleanupCursorKey,
	); !errors.Is(err, failure) {
		t.Fatalf("cursor error = %v", err)
	}
}

func assertNewsCleanupStoreFailure(t *testing.T, failure error) {
	t.Helper()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	engine.putErrors[cleanupBucket] = failure
	if err := pool.storeCleanupCursor(
		t.Context(), knownCleanupCursorKey, vault.Key("row"),
	); !errors.Is(err, failure) {
		t.Fatalf("cursor error = %v", err)
	}
}

func assertNewsCleanupClearFailure(t *testing.T, failure error) {
	t.Helper()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	engine.buckets[cleanupBucket][string(knownCleanupCursorKey)] = []byte("row")
	engine.deleteErrors[cleanupBucket] = failure
	if err := pool.clearCleanupCursor(
		t.Context(), knownCleanupCursorKey,
	); !errors.Is(err, failure) {
		t.Fatalf("cursor error = %v", err)
	}
}

func assertNewsCleanupSequenceClearFailure(t *testing.T, failure error) {
	t.Helper()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	engine.buckets[cleanupBucket][string(categoryCleanupCursorKey)] = []byte("row")
	engine.deleteErrors[cleanupBucket] = failure
	if err := pool.clearCleanupCursors(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("cursor error = %v", err)
	}
}

func TestNewsAdmissionCodecAndStorageFailures(t *testing.T) {
	record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
	valid, _ := json.Marshal(storedNewsAdmission{
		Wire: []byte(record.WireForm()), Destinations: []Queue{Incoming},
	})
	tests := []struct {
		name string
		raw  string
	}{
		{name: "json", raw: "{"},
		{name: "record", raw: string(mustNewsJSON(t, storedNewsAdmission{
			Wire: []byte("x"), Destinations: []Queue{Incoming},
		}))},
		{name: "empty destinations", raw: string(mustNewsJSON(t, storedNewsAdmission{
			Wire: []byte(record.WireForm()),
		}))},
		{name: "duplicate destination", raw: string(mustNewsJSON(t, storedNewsAdmission{
			Wire: []byte(record.WireForm()), Destinations: []Queue{Incoming, Incoming},
		}))},
		{name: "invalid destination", raw: string(mustNewsJSON(t, storedNewsAdmission{
			Wire: []byte(record.WireForm()), Destinations: []Queue{"invalid"},
		}))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, accepted := decodeNewsAdmission(test.raw); accepted {
				t.Fatal("invalid admission accepted")
			}
		})
	}
	if decoded, accepted := decodeNewsAdmission(string(valid)); !accepted ||
		decoded.record.ID() != record.ID() || !validNewsQueue(Published) || validNewsQueue("invalid") {
		t.Fatalf("valid admission = %#v/%t", decoded, accepted)
	}
	failure := errors.New("admission storage failed")
	t.Run("store", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		engine.putErrors[cleanupBucket] = failure
		if err := pool.storeNewsAdmission(t.Context(), newsAdmission{
			record: record, destinations: []Queue{Incoming},
		}); !errors.Is(err, failure) {
			t.Fatalf("store error = %v", err)
		}
	})
	t.Run("read cancellation", func(t *testing.T) {
		pool := openStubPool(t, newNewsStubEngine())
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, _, err := pool.readNewsAdmission(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("read error = %v", err)
		}
	})
	t.Run("invalid is discarded", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		engine.buckets[cleanupBucket][string(newsAdmissionKey)] = []byte("{")
		_, found, err := pool.readNewsAdmission(t.Context())
		if err != nil || found || len(engine.buckets[cleanupBucket]) != 0 {
			t.Fatalf("invalid admission = %t/%v", found, err)
		}
	})
	t.Run("invalid discard failure", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		engine.buckets[cleanupBucket][string(newsAdmissionKey)] = []byte("{")
		engine.deleteErrors[cleanupBucket] = failure
		if _, _, err := pool.readNewsAdmission(t.Context()); !errors.Is(err, failure) {
			t.Fatalf("read error = %v", err)
		}
	})
	t.Run("clear failure", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		engine.buckets[cleanupBucket][string(newsAdmissionKey)] = valid
		engine.deleteErrors[cleanupBucket] = failure
		if err := pool.clearNewsAdmission(t.Context()); !errors.Is(err, failure) {
			t.Fatalf("clear error = %v", err)
		}
	})
}

func TestNewsAdmissionRecoveryFailureStages(t *testing.T) {
	record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
	failure := errors.New("admission recovery failed")
	t.Run("read", func(t *testing.T) {
		assertNewsAdmissionRecoveryReadFailure(t, failure)
	})
	t.Run("dirty reconciliation", func(t *testing.T) {
		assertDirtyNewsAdmissionReconciled(t)
	})
	t.Run("dirty reconciliation failure", func(t *testing.T) {
		assertDirtyNewsAdmissionReconciliationFailure(t, failure)
	})
	t.Run("pending reconciliation", func(t *testing.T) {
		assertPendingNewsAdmissionReconciliationFailure(t, record, failure)
	})
	t.Run("remove identity", func(t *testing.T) {
		assertNewsAdmissionIdentityRemovalFailure(t, record, failure)
	})
	t.Run("replay", func(t *testing.T) {
		assertNewsAdmissionReplayFailure(t, record, failure)
	})
	t.Run("finish", func(t *testing.T) {
		assertNewsAdmissionFinishFailure(t, record, failure)
	})
	t.Run("expired", func(t *testing.T) {
		assertExpiredNewsAdmissionDiscarded(t)
	})
}

func storeNewsAdmissionIntent(t *testing.T, pool *Pool, record Record) {
	t.Helper()
	if err := pool.storeNewsAdmission(t.Context(), newsAdmission{
		record: record, destinations: []Queue{Incoming},
	}); err != nil {
		t.Fatal(err)
	}
}

func assertNewsAdmissionRecoveryReadFailure(t *testing.T, failure error) {
	t.Helper()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	engine.buckets[cleanupBucket][string(newsAdmissionKey)] = []byte("{")
	engine.deleteErrors[cleanupBucket] = failure
	if err := pool.recoverNewsAdmission(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("recovery error = %v", err)
	}
}

func assertDirtyNewsAdmissionReconciled(t *testing.T) {
	t.Helper()
	pool := openStubPool(t, newNewsStubEngine())
	pool.retentionNeedsReconciliation = true
	if err := pool.recoverNewsAdmission(t.Context()); err != nil ||
		pool.retentionNeedsReconciliation {
		t.Fatalf("dirty recovery = %v/%t", err, pool.retentionNeedsReconciliation)
	}
}

func assertDirtyNewsAdmissionReconciliationFailure(t *testing.T, failure error) {
	t.Helper()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	pool.retentionNeedsReconciliation = true
	engine.keyPageFailureOn = true
	engine.keyPageError = failure
	if err := pool.recoverNewsAdmission(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("recovery error = %v", err)
	}
}

func assertPendingNewsAdmissionReconciliationFailure(
	t *testing.T,
	record Record,
	failure error,
) {
	t.Helper()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	storeNewsAdmissionIntent(t, pool, record)
	engine.keyPageFailureOn = true
	engine.keyPageError = failure
	assertNewsAdmissionRecoveryFailedDirty(t, pool, failure)
}

func assertNewsAdmissionIdentityRemovalFailure(t *testing.T, record Record, failure error) {
	t.Helper()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	storeNewsAdmissionIntent(t, pool, record)
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := putKnownNewsFixture(pool, tx, record); err != nil {
			return err
		}
		if err := pool.queue.Put(tx, queueKey(Incoming, 1), record.WireForm()); err != nil {
			return fmt.Errorf("store queued news fixture: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	engine.deleteErrors[queueBucket] = failure
	assertNewsAdmissionRecoveryFailedDirty(t, pool, failure)
}

func assertNewsAdmissionReplayFailure(t *testing.T, record Record, failure error) {
	t.Helper()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	storeNewsAdmissionIntent(t, pool, record)
	engine.putErrors[knownBucket] = failure
	assertNewsAdmissionRecoveryFailedDirty(t, pool, failure)
}

func assertNewsAdmissionFinishFailure(t *testing.T, record Record, failure error) {
	t.Helper()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	storeNewsAdmissionIntent(t, pool, record)
	engine.deleteErrors[cleanupBucket] = failure
	assertNewsAdmissionRecoveryFailedDirty(t, pool, failure)
}

func assertNewsAdmissionRecoveryFailedDirty(t *testing.T, pool *Pool, failure error) {
	t.Helper()
	if err := pool.recoverNewsAdmission(t.Context()); !errors.Is(err, failure) ||
		!pool.retentionNeedsReconciliation {
		t.Fatalf("recovery error = %v dirty=%t", err, pool.retentionNeedsReconciliation)
	}
}

func assertExpiredNewsAdmissionDiscarded(t *testing.T) {
	t.Helper()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	expired := retentionRecord(
		fixedNow().Add(-extendedNewsLifetime-time.Hour), 0, CategoryCrawlStart,
	)
	storeNewsAdmissionIntent(t, pool, expired)
	if err := pool.recoverNewsAdmission(t.Context()); err != nil ||
		len(engine.buckets[knownBucket]) != 0 {
		t.Fatalf("expired recovery = %v with %d rows", err, len(engine.buckets[knownBucket]))
	}
}

func TestNewsStoredReconciliationAndIdentityRemovalFailures(t *testing.T) {
	failure := errors.New("stored news reconciliation failed")
	t.Run("load", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		engine.scanErrors[knownBucket] = failure
		if err := pool.reconcileStoredNews(t.Context()); !errors.Is(err, failure) {
			t.Fatalf("reconcile error = %v", err)
		}
	})
	t.Run("clear cursors", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		engine.buckets[cleanupBucket][string(knownCleanupCursorKey)] = []byte("row")
		engine.deleteErrors[cleanupBucket] = failure
		if err := pool.reconcileStoredNews(t.Context()); !errors.Is(err, failure) {
			t.Fatalf("reconcile error = %v", err)
		}
	})
	t.Run("read queue", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		engine.keyPageFailureOn = true
		engine.keyPageError = failure
		if err := pool.removeNewsIdentity(t.Context(), "id"); !errors.Is(err, failure) {
			t.Fatalf("remove error = %v", err)
		}
	})
	t.Run("delete queue", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
		engine.buckets[queueBucket][string(queueKey(Incoming, 1))] = []byte(record.WireForm())
		engine.deleteErrors[queueBucket] = failure
		if err := pool.removeNewsIdentity(t.Context(), record.ID()); !errors.Is(err, failure) {
			t.Fatalf("remove error = %v", err)
		}
	})
	t.Run("forget identity", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		engine.buckets[knownBucket]["id"] = []byte(knownMarker)
		engine.deleteErrors[knownBucket] = failure
		if err := pool.removeNewsIdentity(t.Context(), "id"); !errors.Is(err, failure) {
			t.Fatalf("remove error = %v", err)
		}
	})
}

func mustNewsJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	return raw
}

func TestNewsPrefixTraversalFailures(t *testing.T) {
	failure := errors.New("prefix traversal failed")
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	engine.keyPageFailureOn = true
	engine.keyPageError = failure
	if err := pool.restoreKnownNewsPrefix(
		t.Context(), fixedNow(), vault.Key("z"), newBoundedNewestNews(1, -1),
	); !errors.Is(err, failure) {
		t.Fatalf("known prefix error = %v", err)
	}
	engine.keyPageFailureOn = false
	engine.keyPageFailureOn = true
	engine.keyPageError = failure
	if err := pool.restoreQueuedNewsPrefix(
		t.Context(), fixedNow(), vault.Key("z"), newBoundedNewestNews(1, 1024), nil,
	); !errors.Is(err, failure) {
		t.Fatalf("queue prefix error = %v", err)
	}
	engine.keyPageFailureOn = false
	engine.buckets[knownCategoryBucket]["category"] = []byte(CategoryCrawlStart)
	if err := pool.validateKnownCategoryPrefix(
		t.Context(), vault.Key("category"),
	); !isStaleNewsCleanupCursor(err) {
		t.Fatalf("category prefix error = %v", err)
	}
	if err := pool.vault.View(t.Context(), func(tx *vault.Txn) error {
		return forNewsKeysThrough(tx, knownBucket, vault.Key("z"), func(vault.Key) error {
			return failure
		})
	}); err != nil {
		t.Fatalf("empty key traversal = %v", err)
	}
	engine.buckets[knownBucket]["a"] = []byte(knownMarker)
	if err := pool.vault.View(t.Context(), func(tx *vault.Txn) error {
		return forNewsKeysThrough(tx, knownBucket, vault.Key("z"), func(vault.Key) error {
			return failure
		})
	}); !errors.Is(err, failure) {
		t.Fatalf("key visitor error = %v", err)
	}
}

func TestNewsRecoveryScansRejectOversizedRowsBeforeValueRead(t *testing.T) {
	oversized := []byte(strings.Repeat("x", maximumNewsRecordBytes+1))
	assertSizeFirst := func(t *testing.T, engine *newsStubEngine) {
		t.Helper()
		if got := engine.valuePageReads[queueBucket]; got != 0 {
			t.Fatalf("value-page reads = %d", got)
		}
		if got := engine.getCalls[queueBucket]; got != 0 {
			t.Fatalf("queue value reads = %d", got)
		}
		if got := engine.valueSizeCalls[queueBucket]; got == 0 {
			t.Fatal("queue encoded size was not inspected")
		}
		if got := engine.keyPageReadsByBucket[queueBucket]; got == 0 {
			t.Fatal("queue keys were not paged")
		}
	}
	reset := func(engine *newsStubEngine) {
		engine.valuePageReads[queueBucket] = 0
		engine.getCalls[queueBucket] = 0
		engine.valueSizeCalls[queueBucket] = 0
		engine.keyPageReadsByBucket[queueBucket] = 0
	}
	t.Run("catalog", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		key := queueKey(Incoming, 7)
		engine.buckets[queueBucket][string(key)] = oversized
		reset(engine)
		catalog, err := pool.buildQueuedNewsEvidenceCatalog(t.Context(), fixedNow())
		if err != nil {
			t.Fatal(err)
		}
		if catalog.latestSequence[Incoming] != 7 {
			t.Fatalf("incoming cursor floor = %d", catalog.latestSequence[Incoming])
		}
		assertSizeFirst(t, engine)
	})
	t.Run("cleanup prefix", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		key := queueKey(Incoming, 1)
		engine.buckets[queueBucket][string(key)] = oversized
		reset(engine)
		err := pool.restoreQueuedNewsPrefix(
			t.Context(), fixedNow(), key, newBoundedNewestNews(1, maximumNewsRecordBytes), nil,
		)
		if !isStaleNewsCleanupCursor(err) {
			t.Fatalf("cleanup prefix error = %v", err)
		}
		assertSizeFirst(t, engine)
	})
	t.Run("admission removal", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		engine.buckets[queueBucket][string(queueKey(Incoming, 1))] = oversized
		reset(engine)
		if err := pool.removeNewsIdentity(t.Context(), "unrelated"); err != nil {
			t.Fatal(err)
		}
		assertSizeFirst(t, engine)
	})
	t.Run("rotation rollback", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
		engine.buckets[queueBucket][string(queueKey(Outgoing, 2))] = oversized
		reset(engine)
		if err := pool.rollbackNewsRotation(t.Context(), newsRotation{
			source: queueKey(Outgoing, 1), original: record,
			rotated: incrementedNewsRecord(record), destination: Outgoing,
		}); err != nil {
			t.Fatal(err)
		}
		assertSizeFirst(t, engine)
	})
}

func TestNewsPersistentSidecarsRejectOversizedRowsBeforeValueRead(t *testing.T) {
	t.Run("known marker", func(t *testing.T) {
		assertNewsKnownMarkerSizeFirst(t)
	})
	t.Run("category evidence", func(t *testing.T) {
		assertNewsCategoryEvidenceSizeFirst(t)
	})
	t.Run("queue cursor", func(t *testing.T) {
		assertNewsQueueCursorSizeFirst(t)
	})
	for _, test := range []struct {
		name string
		key  vault.Key
		read func(*testing.T, *Pool) error
	}{
		{
			name: "cleanup cursor", key: queuedCleanupCursorKey, read: readNewsCleanupCursor,
		},
		{
			name: "admission intent", key: newsAdmissionKey, read: readNewsAdmissionIntent,
		},
		{
			name: "rotation intent", key: newsRotationKey, read: readNewsRotationIntent,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := newNewsStubEngine()
			pool := openStubPool(t, engine)
			engine.buckets[cleanupBucket][string(test.key)] = []byte(
				strings.Repeat("x", maximumNewsCleanupValueBytes+1),
			)
			resetNewsStubValueReads(engine, cleanupBucket)
			if err := test.read(t, pool); err != nil {
				t.Fatal(err)
			}
			assertNewsEncodedSizeRead(t, engine, cleanupBucket)
		})
	}
}

func assertNewsKnownMarkerSizeFirst(t *testing.T) {
	t.Helper()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	engine.buckets[knownBucket]["identity"] = []byte("oversized")
	resetNewsStubValueReads(engine, knownBucket)
	if err := pool.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, err := pool.storedKnownMarkerPresent(tx, vault.Key("identity"))

		return err
	}); err == nil {
		t.Fatal("oversized known marker was accepted")
	}
	assertNewsEncodedSizeRead(t, engine, knownBucket)
}

func assertNewsCategoryEvidenceSizeFirst(t *testing.T) {
	t.Helper()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	engine.buckets[knownCategoryBucket]["identity"] = []byte(
		strings.Repeat("x", maximumKnownCategoryEvidenceBytes+1),
	)
	resetNewsStubValueReads(engine, knownCategoryBucket)
	if err := pool.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, _, err := pool.storedKnownCategoryEvidence(tx, vault.Key("identity"))

		return err
	}); err == nil {
		t.Fatal("oversized category evidence was accepted")
	}
	assertNewsEncodedSizeRead(t, engine, knownCategoryBucket)
}

func assertNewsQueueCursorSizeFirst(t *testing.T) {
	t.Helper()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	engine.buckets[cursorBucket][string(Incoming)] = []byte(strings.Repeat("9", 21))
	resetNewsStubValueReads(engine, cursorBucket)
	if err := pool.raiseQueuedNewsCursors(
		t.Context(), map[Queue]uint64{Incoming: 7},
	); err != nil {
		t.Fatal(err)
	}
	if got := string(engine.buckets[cursorBucket][string(Incoming)]); got != "7" {
		t.Fatalf("repaired cursor = %q", got)
	}
	assertNewsEncodedSizeRead(t, engine, cursorBucket)
}

func resetNewsStubValueReads(engine *newsStubEngine, bucket vault.Name) {
	engine.getCalls[bucket] = 0
	engine.valueSizeCalls[bucket] = 0
}

func assertNewsEncodedSizeRead(t *testing.T, engine *newsStubEngine, bucket vault.Name) {
	t.Helper()
	if got := engine.getCalls[bucket]; got != 0 {
		t.Fatalf("%s value reads = %d", bucket, got)
	}
	if got := engine.valueSizeCalls[bucket]; got == 0 {
		t.Fatalf("%s encoded size was not inspected", bucket)
	}
}

func readNewsCleanupCursor(t *testing.T, pool *Pool) error {
	t.Helper()
	_, err := pool.cleanupCursor(t.Context(), queuedCleanupCursorKey)

	return err
}

func readNewsAdmissionIntent(t *testing.T, pool *Pool) error {
	t.Helper()
	_, _, err := pool.readNewsAdmission(t.Context())

	return err
}

func readNewsRotationIntent(t *testing.T, pool *Pool) error {
	t.Helper()
	_, _, err := pool.readNewsRotation(t.Context())

	return err
}

func TestNewsRotationCodecAndStorageFailures(t *testing.T) {
	record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
	validRotation := newsRotation{
		source: queueKey(Outgoing, 1), original: record,
		rotated: incrementedNewsRecord(record), destination: Outgoing,
	}
	tests := []struct {
		name string
		raw  string
	}{
		{name: "json", raw: "{"},
		{name: "source", raw: string(mustNewsJSON(t, storedNewsRotation{
			Source: queueKey(Incoming, 1), Original: []byte(record.WireForm()),
		}))},
		{name: "record", raw: string(mustNewsJSON(t, storedNewsRotation{
			Source: queueKey(Outgoing, 1), Original: []byte("x"),
		}))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, accepted := decodeNewsRotation(test.raw); accepted {
				t.Fatal("invalid rotation accepted")
			}
		})
	}
	final := record
	final.Distributed = distributionLimit - 1
	raw := mustNewsJSON(t, storedNewsRotation{
		Source: queueKey(Outgoing, 1), Original: []byte(final.WireForm()),
	})
	decoded, accepted := decodeNewsRotation(string(raw))
	if !accepted || decoded.destination != Published ||
		decoded.rotated.Distributed != distributionLimit {
		t.Fatalf("final rotation = %#v/%t", decoded, accepted)
	}
	failure := errors.New("rotation storage failed")
	t.Run("store", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		engine.putErrors[cleanupBucket] = failure
		if err := pool.storeNewsRotation(t.Context(), validRotation); !errors.Is(err, failure) {
			t.Fatalf("store error = %v", err)
		}
	})
	t.Run("read cancellation", func(t *testing.T) {
		pool := openStubPool(t, newNewsStubEngine())
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, _, err := pool.readNewsRotation(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("read error = %v", err)
		}
	})
	t.Run("invalid is discarded", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		engine.buckets[cleanupBucket][string(newsRotationKey)] = []byte("{")
		_, found, err := pool.readNewsRotation(t.Context())
		if err != nil || found || len(engine.buckets[cleanupBucket]) != 0 {
			t.Fatalf("invalid rotation = %t/%v", found, err)
		}
	})
	t.Run("invalid discard failure", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		engine.buckets[cleanupBucket][string(newsRotationKey)] = []byte("{")
		engine.deleteErrors[cleanupBucket] = failure
		if _, _, err := pool.readNewsRotation(t.Context()); !errors.Is(err, failure) {
			t.Fatalf("read error = %v", err)
		}
	})
	t.Run("clear failure", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		engine.buckets[cleanupBucket][string(newsRotationKey)] = []byte("stored")
		engine.deleteErrors[cleanupBucket] = failure
		if err := pool.clearNewsRotation(t.Context()); !errors.Is(err, failure) {
			t.Fatalf("clear error = %v", err)
		}
	})
}

func TestNewsRotationRecoveryFailureStages(t *testing.T) {
	record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
	rotation := newsRotation{
		source: queueKey(Outgoing, 1), original: record,
		rotated: incrementedNewsRecord(record), destination: Outgoing,
	}
	failure := errors.New("rotation recovery failed")
	t.Run("read", func(t *testing.T) {
		assertNewsRotationRecoveryReadFailure(t, failure)
	})
	t.Run("rollback read", func(t *testing.T) {
		assertNewsRotationRollbackReadFailure(t, rotation, failure)
	})
	t.Run("reconcile", func(t *testing.T) {
		assertNewsRotationReconciliationFailure(t, rotation, failure)
	})
	t.Run("finish", func(t *testing.T) {
		assertNewsRotationFinishFailure(t, rotation, failure)
	})
	t.Run("success", func(t *testing.T) {
		assertNewsRotationRecoverySucceeds(t, record, rotation)
	})
}

func storeNewsRotationIntent(t *testing.T, pool *Pool, rotation newsRotation) {
	t.Helper()
	if err := pool.storeNewsRotation(t.Context(), rotation); err != nil {
		t.Fatal(err)
	}
}

func assertNewsRotationRecoveryReadFailure(t *testing.T, failure error) {
	t.Helper()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	engine.buckets[cleanupBucket][string(newsRotationKey)] = []byte("{")
	engine.deleteErrors[cleanupBucket] = failure
	if err := pool.recoverNewsRotation(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("recovery error = %v", err)
	}
}

func assertNewsRotationRollbackReadFailure(
	t *testing.T,
	rotation newsRotation,
	failure error,
) {
	t.Helper()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	storeNewsRotationIntent(t, pool, rotation)
	engine.keyPageFailureOn = true
	engine.keyPageError = failure
	assertNewsRotationRecoveryFailedDirty(t, pool, failure)
}

func assertNewsRotationReconciliationFailure(
	t *testing.T,
	rotation newsRotation,
	failure error,
) {
	t.Helper()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	storeNewsRotationIntent(t, pool, rotation)
	engine.keyPageError = failure
	engine.keyPageLimit = engine.keyPageReads + 1
	assertNewsRotationRecoveryFailedDirty(t, pool, failure)
}

func assertNewsRotationFinishFailure(t *testing.T, rotation newsRotation, failure error) {
	t.Helper()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	storeNewsRotationIntent(t, pool, rotation)
	engine.deleteKeyErrors[cleanupBucket] = map[string]error{
		string(newsRotationKey): failure,
	}
	assertNewsRotationRecoveryFailedDirty(t, pool, failure)
}

func assertNewsRotationRecoveryFailedDirty(t *testing.T, pool *Pool, failure error) {
	t.Helper()
	if err := pool.recoverNewsRotation(t.Context()); !errors.Is(err, failure) ||
		!pool.retentionNeedsReconciliation {
		t.Fatalf("recovery error = %v dirty=%t", err, pool.retentionNeedsReconciliation)
	}
}

func assertNewsRotationRecoverySucceeds(
	t *testing.T,
	record Record,
	rotation newsRotation,
) {
	t.Helper()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	storeNewsRotationIntent(t, pool, rotation)
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := pool.known.Put(tx, vault.Key(record.ID()), knownMarker); err != nil {
			return fmt.Errorf("store known news marker: %w", err)
		}
		if err := pool.replaceKnownNewsCategoryForRecord(
			tx,
			vault.Key(record.ID()),
			record,
		); err != nil {
			return err
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	engine.buckets[queueBucket][string(rotation.source)] = []byte(rotation.original.WireForm())
	engine.buckets[queueBucket][string(queueKey(Outgoing, 2))] = []byte(
		rotation.rotated.WireForm(),
	)
	if err := pool.recoverNewsRotation(t.Context()); err != nil ||
		pool.retentionNeedsReconciliation {
		t.Fatalf("recovery = %v dirty=%t", err, pool.retentionNeedsReconciliation)
	}
	if got := len(engine.buckets[queueBucket]); got != 1 {
		t.Fatalf("recovered queue rows = %d", got)
	}
}

func TestNewsRotationRollbackBoundaries(t *testing.T) {
	record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
	rotation := newsRotation{
		source: queueKey(Outgoing, 1), original: record,
		rotated: incrementedNewsRecord(record), destination: Outgoing,
	}
	failure := errors.New("rotation rollback failed")
	t.Run("delete", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		engine.buckets[queueBucket][string(queueKey(Outgoing, 2))] = []byte(
			rotation.rotated.WireForm(),
		)
		engine.deleteErrors[queueBucket] = failure
		if err := pool.rollbackNewsRotation(t.Context(), rotation); !errors.Is(err, failure) {
			t.Fatalf("rollback error = %v", err)
		}
	})
	t.Run("restore", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		engine.putErrors[queueBucket] = failure
		if err := pool.rollbackNewsRotation(t.Context(), rotation); !errors.Is(err, failure) {
			t.Fatalf("rollback error = %v", err)
		}
	})
	t.Run("bound", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		for index := range maximumNewsQueueRecords + 2 {
			engine.buckets[queueBucket][fmt.Sprintf("invalid-%05d", index)] = []byte("wire")
		}
		if err := pool.rollbackNewsRotation(t.Context(), rotation); err == nil {
			t.Fatal("over-bound rollback succeeded")
		}
	})
	t.Run("skip unrelated and source", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		engine.buckets[queueBucket]["invalid"] = []byte(rotation.rotated.WireForm())
		engine.buckets[queueBucket][string(queueKey(Incoming, 1))] = []byte(
			rotation.rotated.WireForm(),
		)
		engine.buckets[queueBucket][string(rotation.source)] = []byte(rotation.rotated.WireForm())
		if err := pool.rollbackNewsRotation(t.Context(), rotation); err != nil {
			t.Fatal(err)
		}
		if got := len(engine.buckets[queueBucket]); got != 3 {
			t.Fatalf("rollback rows = %d", got)
		}
	})
}
