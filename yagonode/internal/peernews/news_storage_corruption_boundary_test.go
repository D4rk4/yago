package peernews

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestNewsCleanupOperationalReadsDoNotDiscardState(t *testing.T) {
	failure := errors.New("cleanup read failed")
	readers := []struct {
		name string
		key  vault.Key
		read func(*Pool) error
	}{
		{
			name: "cursor",
			key:  knownCleanupCursorKey,
			read: func(pool *Pool) error {
				_, err := pool.cleanupCursor(t.Context(), knownCleanupCursorKey)

				return err
			},
		},
		{
			name: "admission",
			key:  newsAdmissionKey,
			read: func(pool *Pool) error {
				_, _, err := pool.readNewsAdmission(t.Context())

				return err
			},
		},
		{
			name: "rotation",
			key:  newsRotationKey,
			read: func(pool *Pool) error {
				_, _, err := pool.readNewsRotation(t.Context())

				return err
			},
		},
	}
	for _, reader := range readers {
		for _, failureSource := range []string{"size", "value"} {
			t.Run(reader.name+" "+failureSource, func(t *testing.T) {
				engine := newNewsStubEngine()
				pool := openStubPool(t, engine)
				original := []byte("stored state")
				engine.buckets[cleanupBucket][string(reader.key)] = append([]byte(nil), original...)
				resetNewsMutationCalls(engine)
				if failureSource == "size" {
					engine.valueSizeErrors[cleanupBucket] = failure
				} else {
					engine.readErrors[cleanupBucket] = failure
				}
				err := reader.read(pool)
				if !errors.Is(err, failure) || errors.Is(err, vault.ErrCorruptValue) {
					t.Fatalf("read error = %v", err)
				}
				if !bytes.Equal(engine.buckets[cleanupBucket][string(reader.key)], original) {
					t.Fatal("cleanup state changed after operational read failure")
				}
				assertNoNewsMutationCalls(t, engine)
			})
		}
	}
}

func TestNewsRetentionOperationalReadsDoNotMutateState(t *testing.T) {
	failure := errors.New("retention read failed")
	for _, failureSource := range []string{"size", "value"} {
		t.Run("known marker "+failureSource, func(t *testing.T) {
			engine := newNewsStubEngine()
			pool := openStubPool(t, engine)
			record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
			engine.buckets[knownBucket][record.ID()] = []byte(knownMarker)
			resetNewsMutationCalls(engine)
			setNewsReadFailure(engine, knownBucket, failureSource, failure)
			err := pool.pruneKnownNews(t.Context(), fixedNow())
			assertOperationalNewsFailure(t, err, failure, engine)
			if len(engine.buckets[knownBucket]) != 1 {
				t.Fatal("known marker changed after operational read failure")
			}
		})
		t.Run("known category "+failureSource, func(t *testing.T) {
			engine := newNewsStubEngine()
			pool := openStubPool(t, engine)
			record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
			engine.buckets[knownBucket][record.ID()] = []byte(knownMarker)
			engine.buckets[knownCategoryBucket][record.ID()] = []byte(
				knownCategoryEvidence(record),
			)
			resetNewsMutationCalls(engine)
			setNewsReadFailure(engine, knownCategoryBucket, failureSource, failure)
			err := pool.pruneKnownNewsCategories(t.Context())
			assertOperationalNewsFailure(t, err, failure, engine)
			if len(engine.buckets[knownCategoryBucket]) != 1 {
				t.Fatal("known category changed after operational read failure")
			}
		})
		t.Run("queue "+failureSource, func(t *testing.T) {
			engine := newNewsStubEngine()
			pool := openStubPool(t, engine)
			record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
			engine.buckets[queueBucket][string(queueKey(Incoming, 1))] = []byte(
				record.WireForm(),
			)
			resetNewsMutationCalls(engine)
			setNewsReadFailure(engine, queueBucket, failureSource, failure)
			err := pool.pruneQueuedNews(t.Context(), fixedNow())
			assertOperationalNewsFailure(t, err, failure, engine)
			if len(engine.buckets[queueBucket]) != 1 {
				t.Fatal("queue changed after operational read failure")
			}
		})
		t.Run("cursor "+failureSource, func(t *testing.T) {
			engine := newNewsStubEngine()
			pool := openStubPool(t, engine)
			engine.buckets[cursorBucket][string(Incoming)] = []byte("5")
			resetNewsMutationCalls(engine)
			setNewsReadFailure(engine, cursorBucket, failureSource, failure)
			err := pool.raiseQueuedNewsCursors(
				t.Context(),
				map[Queue]uint64{Incoming: 7},
			)
			assertOperationalNewsFailure(t, err, failure, engine)
			if string(engine.buckets[cursorBucket][string(Incoming)]) != "5" {
				t.Fatal("cursor changed after operational read failure")
			}
		})
	}
}

func TestNewsSizeReadMismatchIsCorruption(t *testing.T) {
	t.Run("cleanup is discarded", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		engine.buckets[cleanupBucket][string(knownCleanupCursorKey)] = []byte("row")
		engine.missingReads[cleanupBucket] = map[string]bool{
			string(knownCleanupCursorKey): true,
		}
		cursor, err := pool.cleanupCursor(t.Context(), knownCleanupCursorKey)
		if err != nil || cursor != nil || len(engine.buckets[cleanupBucket]) != 0 {
			t.Fatalf("cleanup cursor = %q, error %v", cursor, err)
		}
	})
	t.Run("cursor is repaired", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		engine.buckets[cursorBucket][string(Incoming)] = []byte("5")
		engine.missingReads[cursorBucket] = map[string]bool{string(Incoming): true}
		if err := pool.raiseQueuedNewsCursors(
			t.Context(),
			map[Queue]uint64{Incoming: 7},
		); err != nil {
			t.Fatal(err)
		}
		if got := string(engine.buckets[cursorBucket][string(Incoming)]); got != "7" {
			t.Fatalf("repaired cursor = %q", got)
		}
	})
}

func setNewsReadFailure(
	engine *newsStubEngine,
	bucket vault.Name,
	source string,
	failure error,
) {
	if source == "size" {
		engine.valueSizeErrors[bucket] = failure

		return
	}
	engine.readErrors[bucket] = failure
}

func resetNewsMutationCalls(engine *newsStubEngine) {
	engine.putCalls = map[vault.Name]int{}
	engine.deleteCalls = map[vault.Name]int{}
}

func assertNoNewsMutationCalls(t *testing.T, engine *newsStubEngine) {
	t.Helper()
	for bucket, calls := range engine.putCalls {
		if calls != 0 {
			t.Fatalf("%s put calls = %d", bucket, calls)
		}
	}
	for bucket, calls := range engine.deleteCalls {
		if calls != 0 {
			t.Fatalf("%s delete calls = %d", bucket, calls)
		}
	}
}

func assertOperationalNewsFailure(
	t *testing.T,
	err error,
	failure error,
	engine *newsStubEngine,
) {
	t.Helper()
	if !errors.Is(err, failure) || errors.Is(err, vault.ErrCorruptValue) {
		t.Fatalf("operation error = %v", err)
	}
	assertNoNewsMutationCalls(t, engine)
}
