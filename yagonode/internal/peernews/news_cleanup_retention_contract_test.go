package peernews

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestKnownNewsCleanupPrefixRejectsInvalidAndUnreadableRows(t *testing.T) {
	failure := errors.New("known prefix storage failed")
	tests := []struct {
		name      string
		key       vault.Key
		configure func(*newsStubEngine)
		stale     bool
	}{
		{
			name: "corrupt marker",
			key:  vault.Key(retentionRecord(fixedNow(), 41, CategoryCrawlStart).ID()),
			configure: func(engine *newsStubEngine) {
				engine.buckets[knownBucket][retentionRecord(
					fixedNow(), 41, CategoryCrawlStart,
				).ID()] = []byte("corrupt")
			},
			stale: true,
		},
		{
			name: "invalid identity",
			key:  vault.Key("invalid"),
			configure: func(engine *newsStubEngine) {
				engine.buckets[knownBucket]["invalid"] = []byte(knownMarker)
			},
			stale: true,
		},
		{
			name: "operational marker failure",
			key:  vault.Key(retentionRecord(fixedNow(), 42, CategoryCrawlStart).ID()),
			configure: func(engine *newsStubEngine) {
				record := retentionRecord(fixedNow(), 42, CategoryCrawlStart)
				engine.buckets[knownBucket][record.ID()] = []byte(knownMarker)
				engine.valueSizeErrors[knownBucket] = failure
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine := newNewsStubEngine()
			pool := openStubPool(t, engine)
			test.configure(engine)
			err := pool.restoreKnownNewsPrefix(
				t.Context(), fixedNow(), test.key, newBoundedNewestNews(4, -1),
			)
			if test.stale && !isStaleNewsCleanupCursor(err) {
				t.Fatalf("known prefix error = %v", err)
			}
			if !test.stale && !errors.Is(err, failure) {
				t.Fatalf("known prefix error = %v, want %v", err, failure)
			}
		})
	}
}

func TestQueuedNewsCleanupPrefixRejectsInvalidAndUnreadableRows(t *testing.T) {
	failure := errors.New("queued prefix storage failed")
	record := retentionRecord(fixedNow(), 51, CategoryCrawlStart)
	key := queueKey(Incoming, 9)
	t.Run("queue read", func(t *testing.T) {
		engine, pool := newNewsCleanupRetentionPool(t)
		seedNewsCleanupQueueRow(engine, key, record)
		engine.readErrors[queueBucket] = failure
		err := pool.restoreQueuedNewsPrefix(
			t.Context(), fixedNow(), key, newBoundedNewestNews(4, 4096),
			newsCleanupEvidence(record, key),
		)
		if !errors.Is(err, failure) {
			t.Fatalf("queued prefix error = %v, want %v", err, failure)
		}
	})
	t.Run("invalid wire", func(t *testing.T) {
		engine, pool := newNewsCleanupRetentionPool(t)
		engine.buckets[queueBucket][string(key)] = []byte("invalid")
		err := pool.restoreQueuedNewsPrefix(
			t.Context(), fixedNow(), key, newBoundedNewestNews(4, 4096), nil,
		)
		if !isStaleNewsCleanupCursor(err) {
			t.Fatalf("queued prefix error = %v", err)
		}
	})
	t.Run("corrupt membership", func(t *testing.T) {
		engine, pool := newNewsCleanupRetentionPool(t)
		seedNewsCleanupQueueRow(engine, key, record)
		engine.buckets[knownBucket][record.ID()] = []byte("corrupt")
		err := pool.restoreQueuedNewsPrefix(
			t.Context(), fixedNow(), key, newBoundedNewestNews(4, 4096),
			newsCleanupEvidence(record, key),
		)
		if !isStaleNewsCleanupCursor(err) {
			t.Fatalf("queued prefix error = %v", err)
		}
	})
	t.Run("membership read", func(t *testing.T) {
		engine, pool := newNewsCleanupRetentionPool(t)
		seedNewsCleanupQueueRow(engine, key, record)
		engine.valueSizeErrors[knownBucket] = failure
		err := pool.restoreQueuedNewsPrefix(
			t.Context(), fixedNow(), key, newBoundedNewestNews(4, 4096),
			newsCleanupEvidence(record, key),
		)
		if !errors.Is(err, failure) {
			t.Fatalf("queued prefix error = %v, want %v", err, failure)
		}
	})
	t.Run("missing membership", func(t *testing.T) {
		engine, pool := newNewsCleanupRetentionPool(t)
		engine.buckets[queueBucket][string(key)] = []byte(record.WireForm())
		err := pool.restoreQueuedNewsPrefix(
			t.Context(), fixedNow(), key, newBoundedNewestNews(4, 4096),
			newsCleanupEvidence(record, key),
		)
		if !isStaleNewsCleanupCursor(err) {
			t.Fatalf("queued prefix error = %v", err)
		}
	})
}

func TestKnownNewsCategoryPrefixPropagatesOperationalFailures(t *testing.T) {
	failure := errors.New("known category prefix failed")
	record := retentionRecord(fixedNow(), 61, CategoryCrawlStart)
	for _, bucket := range []vault.Name{knownCategoryBucket, knownBucket} {
		t.Run(string(bucket), func(t *testing.T) {
			engine, pool := newNewsCleanupRetentionPool(t)
			engine.buckets[knownBucket][record.ID()] = []byte(knownMarker)
			engine.buckets[knownCategoryBucket][record.ID()] = []byte(record.Category)
			engine.valueSizeErrors[bucket] = failure
			if err := pool.validateKnownCategoryPrefix(
				t.Context(), vault.Key(record.ID()),
			); !errors.Is(err, failure) {
				t.Fatalf("category prefix error = %v, want %v", err, failure)
			}
		})
	}
}

func TestNewsCleanupPrefixTraversesEveryBoundedPage(t *testing.T) {
	engine, pool := newNewsCleanupRetentionPool(t)
	for index := range newsScrubPage + 1 {
		key := fmt.Sprintf("%04d", index)
		engine.buckets[knownBucket][key] = []byte(knownMarker)
	}
	engine.keyPageAfterByBucket[knownBucket] = nil
	visited := 0
	err := pool.vault.View(t.Context(), func(tx *vault.Txn) error {
		return forNewsKeysThrough(tx, knownBucket, vault.Key("9999"), func(vault.Key) error {
			visited++
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if visited != newsScrubPage+1 {
		t.Fatalf("visited keys = %d, want %d", visited, newsScrubPage+1)
	}
	if got := engine.keyPageAfterByBucket[knownBucket]; len(got) != 2 || got[1] == "" {
		t.Fatalf("known prefix page cursors = %q", got)
	}
}

func TestKnownNewsStartupCleanupPropagatesCursorReadFailure(t *testing.T) {
	failure := errors.New("known cleanup cursor read failed")
	engine, pool := newNewsCleanupRetentionPool(t)
	engine.valueSizeErrors[cleanupBucket] = failure
	if err := pool.pruneKnownNews(t.Context(), fixedNow()); !errors.Is(err, failure) {
		t.Fatalf("known cleanup error = %v, want %v", err, failure)
	}
}

func TestKnownNewsStartupCleanupPropagatesPrefixFailure(t *testing.T) {
	failure := errors.New("known cleanup prefix read failed")
	engine, pool := newNewsCleanupRetentionPool(t)
	record := retentionRecord(fixedNow(), 71, CategoryCrawlStart)
	engine.buckets[knownBucket][record.ID()] = []byte(knownMarker)
	seedNewsCleanupCursor(engine, knownCleanupCursorKey, vault.Key(record.ID()))
	engine.valueSizeErrors[knownBucket] = failure
	if err := pool.pruneKnownNews(t.Context(), fixedNow()); !errors.Is(err, failure) {
		t.Fatalf("known cleanup error = %v, want %v", err, failure)
	}
}

func TestKnownNewsStartupCleanupReportsStaleCursorResetFailure(t *testing.T) {
	failure := errors.New("known cleanup reset failed")
	engine, pool := newNewsCleanupRetentionPool(t)
	engine.buckets[knownBucket]["invalid"] = []byte(knownMarker)
	seedNewsCleanupCursor(engine, knownCleanupCursorKey, vault.Key("invalid"))
	engine.deleteErrors[cleanupBucket] = failure
	if err := pool.pruneKnownNews(t.Context(), fixedNow()); !errors.Is(err, failure) {
		t.Fatalf("known cleanup error = %v, want %v", err, failure)
	}
}

func TestKnownNewsStartupCleanupRestartsAfterStaleCursor(t *testing.T) {
	engine, pool := newNewsCleanupRetentionPool(t)
	engine.buckets[knownBucket]["invalid"] = []byte(knownMarker)
	seedNewsCleanupCursor(engine, knownCleanupCursorKey, vault.Key("invalid"))
	if err := pool.pruneKnownNews(t.Context(), fixedNow()); err != nil {
		t.Fatal(err)
	}
	if _, found := engine.buckets[knownBucket]["invalid"]; found {
		t.Fatal("invalid known row survived stale-cursor restart")
	}
}

func TestKnownNewsStartupCleanupReportsCheckpointFailure(t *testing.T) {
	failure := errors.New("known cleanup checkpoint failed")
	engine, pool := newNewsCleanupRetentionPool(t)
	record := retentionRecord(fixedNow(), 72, CategoryCrawlStart)
	engine.buckets[knownBucket][record.ID()] = []byte(knownMarker)
	engine.putErrors[cleanupBucket] = failure
	if err := pool.pruneKnownNews(t.Context(), fixedNow()); !errors.Is(err, failure) {
		t.Fatalf("known cleanup error = %v, want %v", err, failure)
	}
}

func TestQueuedNewsStartupCleanupPropagatesCursorFloorFailure(t *testing.T) {
	failure := errors.New("queued cursor floor failed")
	engine, pool := newNewsCleanupRetentionPool(t)
	engine.buckets[queueBucket][string(queueKey(Incoming, 81))] = []byte("invalid")
	engine.putErrors[cursorBucket] = failure
	if err := pool.pruneQueuedNews(t.Context(), fixedNow()); !errors.Is(err, failure) {
		t.Fatalf("queued cleanup error = %v, want %v", err, failure)
	}
}

func TestQueuedNewsStartupCleanupReportsCheckpointFailure(t *testing.T) {
	failure := errors.New("queued cleanup checkpoint failed")
	engine, pool := newNewsCleanupRetentionPool(t)
	engine.buckets[queueBucket][string(queueKey(Incoming, 82))] = []byte("invalid")
	engine.putErrors[cleanupBucket] = failure
	if err := pool.pruneQueuedNews(t.Context(), fixedNow()); !errors.Is(err, failure) {
		t.Fatalf("queued cleanup error = %v, want %v", err, failure)
	}
}

func TestQueuedNewsStartupCleanupPropagatesPrefixFailure(t *testing.T) {
	failure := errors.New("queued cleanup prefix failed")
	engine, pool := newNewsCleanupRetentionPool(t)
	record := retentionRecord(fixedNow(), 83, CategoryCrawlStart)
	key := queueKey(Incoming, 83)
	seedNewsCleanupQueueRow(engine, key, record)
	seedNewsCleanupCursor(engine, queuedCleanupCursorKey, key)
	views := 0
	engine.beforeView = func() {
		views++
		if views == 3 {
			engine.valueSizeErrors[queueBucket] = failure
		}
	}
	if err := pool.pruneQueuedNews(t.Context(), fixedNow()); !errors.Is(err, failure) {
		t.Fatalf("queued cleanup error = %v, want %v", err, failure)
	}
}

func TestQueuedNewsStartupCleanupReportsStaleCursorResetFailure(t *testing.T) {
	failure := errors.New("queued cleanup reset failed")
	engine, pool := newNewsCleanupRetentionPool(t)
	key := queueKey(Incoming, 84)
	engine.buckets[queueBucket][string(key)] = []byte("invalid")
	seedNewsCleanupCursor(engine, queuedCleanupCursorKey, key)
	engine.deleteErrors[cleanupBucket] = failure
	if err := pool.pruneQueuedNews(t.Context(), fixedNow()); !errors.Is(err, failure) {
		t.Fatalf("queued cleanup error = %v, want %v", err, failure)
	}
}

func TestQueuedNewsStartupCleanupRestartsAfterStaleCursor(t *testing.T) {
	engine, pool := newNewsCleanupRetentionPool(t)
	key := queueKey(Incoming, 85)
	engine.buckets[queueBucket][string(key)] = []byte("invalid")
	seedNewsCleanupCursor(engine, queuedCleanupCursorKey, key)
	if err := pool.pruneQueuedNews(t.Context(), fixedNow()); err != nil {
		t.Fatal(err)
	}
	if _, found := engine.buckets[queueBucket][string(key)]; found {
		t.Fatal("invalid queued row survived stale-cursor restart")
	}
}

func TestQueuedNewsPagePropagatesCancellationAndReadFailure(t *testing.T) {
	t.Run("cancellation", func(t *testing.T) {
		engine, pool := newNewsCleanupRetentionPool(t)
		ctx, cancel := context.WithCancel(t.Context())
		engine.beforeUpdate = cancel
		_, err := pool.pruneQueuedNewsPage(
			ctx, fixedNow(), nil, newBoundedNewestNews(4, 4096), nil,
		)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queued page error = %v", err)
		}
	})
	t.Run("key page", func(t *testing.T) {
		failure := errors.New("queued page read failed")
		engine, pool := newNewsCleanupRetentionPool(t)
		engine.keyPageFailureOn = true
		engine.keyPageError = failure
		_, err := pool.pruneQueuedNewsPage(
			t.Context(), fixedNow(), nil, newBoundedNewestNews(4, 4096), nil,
		)
		if !errors.Is(err, failure) {
			t.Fatalf("queued page error = %v, want %v", err, failure)
		}
	})
}

func TestQueuedNewsRetentionPropagatesOperationalFailures(t *testing.T) {
	failure := errors.New("queued retention storage failed")
	record := retentionRecord(fixedNow(), 91, CategoryCrawlStart)
	key := queueKey(Incoming, 91)
	t.Run("queue inspection", func(t *testing.T) {
		engine, pool := newNewsCleanupRetentionPool(t)
		seedNewsCleanupQueueRow(engine, key, record)
		engine.valueSizeErrors[queueBucket] = failure
		err := viewNewsCleanupRetention(t, pool, func(tx *vault.Txn) error {
			_, _, err := pool.retainedQueuedNewsRecord(
				tx, key, fixedNow(), newsCleanupEvidence(record, key),
			)
			return err
		})
		if !errors.Is(err, failure) {
			t.Fatalf("queued retention error = %v, want %v", err, failure)
		}
	})
	t.Run("row disappears after inspection", func(t *testing.T) {
		engine, pool := newNewsCleanupRetentionPool(t)
		seedNewsCleanupQueueRow(engine, key, record)
		engine.missingReads[queueBucket] = map[string]bool{string(key): true}
		err := viewNewsCleanupRetention(t, pool, func(tx *vault.Txn) error {
			_, _, _, err := pool.readQueuedNewsWire(tx, key)
			return err
		})
		if !errors.Is(err, vault.ErrCorruptValue) {
			t.Fatalf("queued retention error = %v", err)
		}
	})
	t.Run("known membership", func(t *testing.T) {
		engine, pool := newNewsCleanupRetentionPool(t)
		seedNewsCleanupQueueRow(engine, key, record)
		engine.valueSizeErrors[knownBucket] = failure
		err := viewNewsCleanupRetention(t, pool, func(tx *vault.Txn) error {
			_, err := pool.reconcileQueuedNewsMembership(
				tx, record, fixedNow(), newsCleanupEvidence(record, key),
			)
			return err
		})
		if !errors.Is(err, failure) {
			t.Fatalf("queued retention error = %v, want %v", err, failure)
		}
	})
	t.Run("category membership", func(t *testing.T) {
		engine, pool := newNewsCleanupRetentionPool(t)
		seedNewsCleanupQueueRow(engine, key, record)
		engine.valueSizeErrors[knownCategoryBucket] = failure
		err := viewNewsCleanupRetention(t, pool, func(tx *vault.Txn) error {
			_, err := pool.reconcileQueuedNewsMembership(
				tx, record, fixedNow(), newsCleanupEvidence(record, key),
			)
			return err
		})
		if !errors.Is(err, failure) {
			t.Fatalf("queued retention error = %v, want %v", err, failure)
		}
	})
}

func newNewsCleanupRetentionPool(t *testing.T) (*newsStubEngine, *Pool) {
	t.Helper()
	engine := newNewsStubEngine()
	return engine, openStubPool(t, engine)
}

func seedNewsCleanupCursor(engine *newsStubEngine, name, after vault.Key) {
	engine.buckets[cleanupBucket][string(name)] = append([]byte(nil), after...)
}

func seedNewsCleanupQueueRow(engine *newsStubEngine, key vault.Key, record Record) {
	engine.buckets[queueBucket][string(key)] = []byte(record.WireForm())
	engine.buckets[knownBucket][record.ID()] = []byte(knownMarker)
	engine.buckets[knownCategoryBucket][record.ID()] = []byte(record.Category)
}

func newsCleanupEvidence(record Record, key vault.Key) queuedNewsEvidenceCatalog {
	return queuedNewsEvidenceCatalog{
		record.ID(): {
			category: record.Category, generation: knownCategoryGeneration(record),
			key: append(vault.Key(nil), key...), priority: 2,
		},
	}
}

func viewNewsCleanupRetention(
	t *testing.T,
	pool *Pool,
	read func(*vault.Txn) error,
) error {
	t.Helper()
	if err := pool.vault.View(t.Context(), read); err != nil {
		return fmt.Errorf("view news cleanup retention: %w", err)
	}

	return nil
}
