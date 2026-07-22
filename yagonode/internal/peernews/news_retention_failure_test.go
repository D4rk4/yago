package peernews

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestBoundedNewestNewsHandlesStableTiesReplacementAndDisabledLimits(t *testing.T) {
	created := fixedNow()
	left := retainedNewsRecord{
		key: vault.Key("a"), tie: vault.Key("same"), created: created, bytes: 4,
	}
	right := retainedNewsRecord{
		key: vault.Key("b"), tie: vault.Key("same"), created: created, bytes: 4,
	}
	if !retainedNewsBefore(left, right) || retainedNewsBefore(right, left) {
		t.Fatal("stable key ordering mismatch")
	}
	for _, bounds := range [][2]int{{0, 8}, {8, 0}} {
		retained := newBoundedNewestNews(bounds[0], bounds[1])
		removed := retained.Add(left)
		if len(removed) != 1 || retained.records.Len() != 0 {
			t.Fatalf("disabled bounds %v retained %#v", bounds, retained.records)
		}
	}
	retained := newBoundedNewestNews(2, 5)
	retained.Add(left)
	newerLeft := left
	newerLeft.created = created.Add(time.Second)
	retained.Add(newerLeft)
	removed := retained.Add(right)
	if len(removed) != 1 || string(removed[0].key) != "b" ||
		retained.records.Len() != 1 || !retained.Contains(vault.Key("a")) {
		t.Fatalf("byte-bound replacement = removed %#v, retained %#v", removed, retained.records)
	}
}

func TestNewsRetentionParsesIdentityAndLegacyTimeEdges(t *testing.T) {
	if decoded, err := (knownCodec{}).Decode([]byte(knownMarker)); err != nil ||
		decoded != knownMarker {
		t.Fatalf("known marker = %q/%v", decoded, err)
	}
	validHash := yagomodel.WordHash("peer").String()
	for _, id := range []string{
		"short",
		"20260721120000" + strings.Repeat("!", yagomodel.HashLength),
		"20261340129999" + validHash,
	} {
		if _, err := newsIDCreation(id); err == nil {
			t.Fatalf("invalid news id %q accepted", id)
		}
	}
	fallback := fixedNow().Add(-time.Minute)
	wire := "{cat=TestCat,cre=20260721120000,dis=0,ori=" + validHash + ",rec=invalid}"
	record, err := parseRecord(wire, fallback)
	if err != nil {
		t.Fatal(err)
	}
	if !record.Received.Equal(fallback.UTC().Truncate(time.Second)) {
		t.Fatalf("received fallback = %v, want %v", record.Received, fallback)
	}
}

func TestNewsPoolWriteOperationsHonorPermitAndLoopCancellation(t *testing.T) {
	pool := openMemPool(t)
	if err := pool.writePermit.Acquire(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := pool.PublishOwnNews(
		ctx, yagomodel.WordHash("self"), CategoryCrawlStart, nil,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("publish error = %v, want cancellation", err)
	}
	if _, _, err := pool.NextPublication(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("publication error = %v, want cancellation", err)
	}
	pool.writePermit.Release()

	if err := pool.PublishOwnNews(
		t.Context(), yagomodel.WordHash("self"), CategoryCrawlStart,
		map[string]string{"payload": strings.Repeat("x", maximumNewsRecordBytes)},
	); !errors.Is(err, ErrBadNewsRecord) {
		t.Fatalf("oversized publish error = %v", err)
	}

	ctx, cancel = context.WithCancel(t.Context())
	pool.now = func() time.Time {
		cancel()

		return fixedNow()
	}
	if _, _, err := pool.NextPublication(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("loop cancellation error = %v", err)
	}
}

func TestNewsReadPathsRejectCorruptMembershipAndOrderEqualTimes(t *testing.T) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
	if stored, err := pool.EnqueueIncomingNews(t.Context(), record); err != nil || !stored {
		t.Fatalf("enqueue = %t/%v", stored, err)
	}
	engine.buckets[knownBucket][record.ID()] = []byte("corrupt")
	if _, _, err := pool.ByID(t.Context(), Incoming, record.ID()); err == nil {
		t.Fatal("ByID accepted corrupt membership")
	}
	if _, err := pool.Recent(t.Context(), Incoming, 1); err == nil {
		t.Fatal("Recent accepted corrupt membership")
	}

	pool = openMemPool(t)
	legacy := Record{
		Originator: yagomodel.WordHash("legacy"), Created: fixedNow().Add(-time.Hour),
		Attributes: map[string]string{},
	}
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := pool.known.Put(tx, vault.Key(legacy.ID()), knownMarker); err != nil {
			return fmt.Errorf("put legacy membership: %w", err)
		}

		return pool.queue.Put(tx, queueKey(Incoming, 1), legacy.WireForm())
	}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := pool.ByID(t.Context(), Incoming, legacy.ID()); err != nil || !found {
		t.Fatalf("legacy membership = %t/%v", found, err)
	}

	expired := retentionRecord(
		fixedNow().Add(-extendedNewsLifetime-time.Second),
		0,
		CategoryCrawlStart,
	)
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := putKnownNewsFixture(pool, tx, expired); err != nil {
			return fmt.Errorf("put expired membership: %w", err)
		}

		return pool.queue.Put(tx, queueKey(Incoming, 2), expired.WireForm())
	}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := pool.ByID(t.Context(), Incoming, expired.ID()); err != nil || found {
		t.Fatalf("expired membership = %t/%v", found, err)
	}

	pool = openMemPool(t)
	created := fixedNow().Add(-time.Hour)
	records := []Record{
		{Originator: yagomodel.WordHash("peer-b"), Created: created, Category: CategoryCrawlStart},
		{Originator: yagomodel.WordHash("peer-a"), Created: created, Category: CategoryCrawlStart},
	}
	for _, candidate := range records {
		if stored, err := pool.EnqueueIncomingNews(t.Context(), candidate); err != nil || !stored {
			t.Fatalf("enqueue tied record = %t/%v", stored, err)
		}
	}
	recent, err := pool.Recent(t.Context(), Incoming, 2)
	if err != nil {
		t.Fatal(err)
	}
	if recent[0].ID() > recent[1].ID() {
		t.Fatalf("equal-time order = %q before %q", recent[0].ID(), recent[1].ID())
	}
}

func TestNewsPublicationRejectsCorruptMembershipAndCursorFailure(t *testing.T) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	if err := pool.PublishOwnNews(
		t.Context(), yagomodel.WordHash("self"), "TestCat", nil,
	); err != nil {
		t.Fatal(err)
	}
	for id := range engine.buckets[knownBucket] {
		engine.buckets[knownBucket][id] = []byte("corrupt")
	}
	if _, _, err := pool.NextPublication(t.Context()); err == nil {
		t.Fatal("publication accepted corrupt membership")
	}

	engine = newNewsStubEngine()
	pool = openStubPool(t, engine)
	if err := pool.PublishOwnNews(
		t.Context(), yagomodel.WordHash("self"), "TestCat", nil,
	); err != nil {
		t.Fatal(err)
	}
	engine.putErrors[cursorBucket] = errors.New("cursor failed")
	if _, _, err := pool.NextPublication(t.Context()); err == nil {
		t.Fatal("publication cursor failure was ignored")
	}
}

func TestNewsStoreHonorsKnownAndQueueRetentionAdmission(t *testing.T) {
	base := fixedNow().Add(-time.Hour)
	t.Run("known disabled", func(t *testing.T) {
		pool := openMemPool(t)
		pool.retention.knownRecords = 0
		stored, err := pool.EnqueueIncomingNews(
			t.Context(), retentionRecord(base, 0, CategoryCrawlStart),
		)
		if err != nil || stored {
			t.Fatalf("stored = %t/%v", stored, err)
		}
	})
	t.Run("queue disabled", func(t *testing.T) {
		pool := openMemPool(t)
		pool.retention.queueRecords = 0
		stored, err := pool.EnqueueIncomingNews(
			t.Context(), retentionRecord(base, 0, CategoryCrawlStart),
		)
		if err != nil || stored {
			t.Fatalf("stored = %t/%v", stored, err)
		}
	})
	t.Run("one of multiple destinations", func(t *testing.T) {
		pool := openMemPool(t)
		pool.retention.queueRecords = 1
		if err := pool.PublishOwnNews(
			t.Context(), yagomodel.WordHash("self"), CategoryCrawlStart, nil,
		); err != nil {
			t.Fatal(err)
		}
		if pool.stored.queueRecords != 1 {
			t.Fatalf("queued records = %d", pool.stored.queueRecords)
		}
	})
	plan := prepareNewsRetention(newBoundedNewestNews(1, 1), nil, nil)
	if plan.retains(vault.Key("missing")) {
		t.Fatal("missing pending record retained")
	}
}

func TestNewsStorePropagatesRetentionDeletionFailures(t *testing.T) {
	failure := errors.New("retention delete failed")
	t.Run("known", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		expired := retentionRecord(
			fixedNow().Add(-extendedNewsLifetime-time.Second), 0, CategoryCrawlStart,
		)
		if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return putKnownNewsFixture(pool, tx, expired)
		}); err != nil {
			t.Fatal(err)
		}
		engine.deleteErrors[knownBucket] = failure
		if _, err := pool.storeNewsRecord(
			t.Context(), expired, fixedNow(), []Queue{Incoming},
		); !errors.Is(err, failure) {
			t.Fatalf("store error = %v, want %v", err, failure)
		}
	})
	t.Run("queue", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		pool.retention.queueRecords = 1
		base := fixedNow().Add(-time.Hour)
		if stored, err := pool.EnqueueIncomingNews(
			t.Context(), retentionRecord(base, 0, CategoryCrawlStart),
		); err != nil || !stored {
			t.Fatalf("first enqueue = %t/%v", stored, err)
		}
		engine.deleteErrors[queueBucket] = failure
		if _, err := pool.EnqueueIncomingNews(
			t.Context(), retentionRecord(base, 1, CategoryCrawlStart),
		); !errors.Is(err, failure) {
			t.Fatalf("second enqueue error = %v, want %v", err, failure)
		}
	})
}

func TestNewsOpenReportsPruneAndStoredStateFailures(t *testing.T) {
	failure := errors.New("startup prune failed")
	t.Run("prune", func(t *testing.T) {
		engine := newNewsStubEngine()
		storage, err := vault.New(engine)
		if err != nil {
			t.Fatal(err)
		}
		if err := engine.Provision(knownBucket); err != nil {
			t.Fatal(err)
		}
		engine.buckets[knownBucket]["invalid"] = []byte(knownMarker)
		engine.deleteErrors[knownBucket] = failure
		if _, err := Open(storage, fixedNow); !errors.Is(err, failure) {
			t.Fatalf("Open error = %v, want %v", err, failure)
		}
	})
	t.Run("load", func(t *testing.T) {
		engine := newNewsStubEngine()
		storage, err := vault.New(engine)
		if err != nil {
			t.Fatal(err)
		}
		engine.buckets[vault.Name("__lengths__")][string(knownBucket)] = []byte("bad")
		if _, err := Open(storage, fixedNow); err == nil {
			t.Fatal("Open accepted corrupt retained length")
		}
	})
}

func TestLoadStoredNewsStatePropagatesCorruptionAndCancellation(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*Pool, *newsStubEngine, context.CancelFunc)
	}{
		{
			name: "known length",
			configure: func(_ *Pool, engine *newsStubEngine, _ context.CancelFunc) {
				engine.buckets[vault.Name("__lengths__")][string(knownBucket)] = []byte("bad")
			},
		},
		{
			name: "queue length",
			configure: func(_ *Pool, engine *newsStubEngine, _ context.CancelFunc) {
				engine.buckets[vault.Name("__lengths__")][string(queueBucket)] = []byte("bad")
			},
		},
		{
			name: "known scan",
			configure: func(_ *Pool, engine *newsStubEngine, _ context.CancelFunc) {
				engine.scanErrors[knownBucket] = errors.New("known scan failed")
			},
		},
		{
			name: "known identity",
			configure: func(_ *Pool, engine *newsStubEngine, _ context.CancelFunc) {
				engine.buckets[knownBucket]["invalid"] = []byte(knownMarker)
			},
		},
		{
			name: "known cancellation",
			configure: func(_ *Pool, engine *newsStubEngine, cancel context.CancelFunc) {
				id := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart).ID()
				engine.buckets[knownBucket][id] = []byte(knownMarker)
				engine.beforeScans[knownBucket] = cancel
			},
		},
		{
			name: "queue scan",
			configure: func(_ *Pool, engine *newsStubEngine, _ context.CancelFunc) {
				engine.scanErrors[queueBucket] = errors.New("queue scan failed")
			},
		},
		{
			name: "queue record",
			configure: func(_ *Pool, engine *newsStubEngine, _ context.CancelFunc) {
				engine.buckets[queueBucket][string(queueKey(Incoming, 1))] = []byte("invalid")
			},
		},
		{
			name: "queue cancellation",
			configure: func(_ *Pool, engine *newsStubEngine, cancel context.CancelFunc) {
				record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
				engine.buckets[queueBucket][string(queueKey(Incoming, 1))] = []byte(
					record.WireForm(),
				)
				engine.beforeScans[queueBucket] = cancel
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine := newNewsStubEngine()
			pool := openStubPool(t, engine)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			test.configure(pool, engine, cancel)
			if err := pool.loadStoredState(ctx); err == nil {
				t.Fatal("loadStoredState succeeded")
			}
		})
	}
}

func TestNewsStartupPruningPropagatesCancellationAndStorageFailures(t *testing.T) {
	failure := errors.New("startup retention failed")
	testNewsStartupPruningCancellation(t)
	testNewsStartupPruningPageFailures(t, failure)
	testNewsStartupKnownRetentionFailures(t, failure)
	testNewsStartupMembershipFailures(t, failure)
	testNewsStartupQueueRetentionFailures(t, failure)
}

func testNewsStartupPruningCancellation(t *testing.T) {
	t.Helper()
	t.Run("known cancellation", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		ctx, cancel := context.WithCancel(t.Context())
		engine.beforeUpdate = cancel
		if err := pool.pruneKnownNews(ctx, fixedNow()); !errors.Is(err, context.Canceled) {
			t.Fatalf("known prune error = %v", err)
		}
	})
	t.Run("queued cancellation", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		ctx, cancel := context.WithCancel(t.Context())
		engine.beforeUpdate = cancel
		if err := pool.pruneQueuedNews(ctx, fixedNow()); !errors.Is(err, context.Canceled) {
			t.Fatalf("queued prune error = %v", err)
		}
	})
}

func testNewsStartupPruningPageFailures(t *testing.T, failure error) {
	t.Helper()
	for _, queue := range []bool{false, true} {
		name := "known page"
		if queue {
			name = "queued page"
		}
		t.Run(name, func(t *testing.T) {
			engine := newNewsStubEngine()
			pool := openStubPool(t, engine)
			engine.keyPageFailureOn = true
			engine.keyPageError = failure
			var err error
			if queue {
				err = pool.pruneQueuedNews(t.Context(), fixedNow())
			} else {
				err = pool.pruneKnownNews(t.Context(), fixedNow())
			}
			if !errors.Is(err, failure) {
				t.Fatalf("page error = %v, want %v", err, failure)
			}
		})
	}
}

func testNewsStartupKnownRetentionFailures(t *testing.T, failure error) {
	t.Helper()
	t.Run("invalid known delete", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		engine.buckets[knownBucket]["invalid"] = []byte(knownMarker)
		engine.deleteErrors[knownBucket] = failure
		if err := pool.pruneKnownNews(t.Context(), fixedNow()); !errors.Is(err, failure) {
			t.Fatalf("known delete error = %v", err)
		}
	})
	t.Run("bounded known delete", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
		engine.buckets[knownBucket][record.ID()] = []byte(knownMarker)
		engine.buckets[knownCategoryBucket][record.ID()] = []byte(record.Category)
		pool.retention.knownRecords = 0
		engine.deleteErrors[knownBucket] = failure
		if err := pool.pruneKnownNews(t.Context(), fixedNow()); !errors.Is(err, failure) {
			t.Fatalf("bounded known error = %v", err)
		}
	})
}

func testNewsStartupMembershipFailures(t *testing.T, failure error) {
	t.Helper()
	t.Run("known membership decode", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
		engine.buckets[knownBucket][record.ID()] = []byte("corrupt")
		engine.buckets[queueBucket][string(queueKey(Incoming, 1))] = []byte(record.WireForm())
		if err := pool.pruneQueuedNews(t.Context(), fixedNow()); err != nil {
			t.Fatalf("prune corrupt known membership: %v", err)
		}
		if len(engine.buckets[queueBucket]) != 0 {
			t.Fatal("queue row with corrupt known membership retained")
		}
	})
	t.Run("legacy category migration", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
		engine.buckets[knownBucket][record.ID()] = []byte(knownMarker)
		engine.buckets[queueBucket][string(queueKey(Incoming, 1))] = []byte(record.WireForm())
		engine.putErrors[knownCategoryBucket] = failure
		if err := pool.pruneQueuedNews(t.Context(), fixedNow()); !errors.Is(err, failure) {
			t.Fatalf("migration error = %v", err)
		}
	})
	t.Run("expired legacy membership delete", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		record := retentionRecord(fixedNow().Add(-25*time.Hour), 0, CategoryBookmarkAdd)
		engine.buckets[knownBucket][record.ID()] = []byte(knownMarker)
		engine.buckets[queueBucket][string(queueKey(Incoming, 1))] = []byte(record.WireForm())
		engine.deleteErrors[knownBucket] = failure
		if err := pool.pruneQueuedNews(t.Context(), fixedNow()); !errors.Is(err, failure) {
			t.Fatalf("legacy delete error = %v", err)
		}
	})
}

func testNewsStartupQueueRetentionFailures(t *testing.T, failure error) {
	t.Helper()
	t.Run("invalid queue delete", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		engine.buckets[queueBucket][string(queueKey(Incoming, 1))] = []byte("invalid")
		engine.deleteErrors[queueBucket] = failure
		if err := pool.pruneQueuedNews(t.Context(), fixedNow()); !errors.Is(err, failure) {
			t.Fatalf("invalid queue delete error = %v", err)
		}
	})
	t.Run("bounded queue delete", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
		engine.buckets[knownBucket][record.ID()] = []byte(knownMarker)
		engine.buckets[knownCategoryBucket][record.ID()] = []byte(record.Category)
		engine.buckets[queueBucket][string(queueKey(Incoming, 1))] = []byte(record.WireForm())
		pool.retention.queueRecords = 0
		engine.deleteErrors[queueBucket] = failure
		if err := pool.pruneQueuedNews(t.Context(), fixedNow()); !errors.Is(err, failure) {
			t.Fatalf("bounded queue error = %v", err)
		}
	})
}

func TestInspectKnownNewsRejectsMalformedIdentity(t *testing.T) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	engine.buckets[knownBucket]["invalid"] = []byte(knownMarker)
	if _, _, err := pool.inspectKnownNews(t.Context(), "invalid", fixedNow()); err == nil {
		t.Fatal("malformed known identity accepted")
	}
}

func TestKnownNewsCategoryStorageFailuresAreReported(t *testing.T) {
	failure := errors.New("known category storage failed")
	t.Run("clear category", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		engine.buckets[knownCategoryBucket]["record"] = []byte(CategoryCrawlStart)
		engine.deleteErrors[knownCategoryBucket] = failure
		if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return pool.replaceKnownNewsCategory(tx, vault.Key("record"), "")
		}); !errors.Is(err, failure) {
			t.Fatalf("clear category error = %v", err)
		}
	})
	t.Run("forget category", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		engine.buckets[knownCategoryBucket]["record"] = []byte(CategoryCrawlStart)
		engine.deleteErrors[knownCategoryBucket] = failure
		if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return pool.forgetKnownNews(tx, vault.Key("record"))
		}); !errors.Is(err, failure) {
			t.Fatalf("forget category error = %v", err)
		}
	})
	t.Run("corrupt category discard", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
		engine.buckets[knownBucket][record.ID()] = []byte(knownMarker)
		engine.buckets[knownCategoryBucket][record.ID()] = []byte("corrupt-category")
		engine.deleteErrors[knownCategoryBucket] = failure
		if err := pool.pruneKnownNews(t.Context(), fixedNow()); !errors.Is(err, failure) {
			t.Fatalf("corrupt category error = %v", err)
		}
	})
	t.Run("orphan category discard", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		engine.buckets[knownCategoryBucket]["orphan"] = []byte(CategoryCrawlStart)
		engine.deleteErrors[knownCategoryBucket] = failure
		if err := pool.prune(t.Context()); !errors.Is(err, failure) {
			t.Fatalf("orphan category error = %v", err)
		}
	})
}

func TestKnownNewsCategoryCleanupReportsCancellationAndPageFailure(t *testing.T) {
	t.Run("cancellation", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		ctx, cancel := context.WithCancel(t.Context())
		engine.beforeUpdate = cancel
		if err := pool.pruneKnownNewsCategories(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("category cancellation error = %v", err)
		}
	})
	t.Run("page", func(t *testing.T) {
		engine := newNewsStubEngine()
		pool := openStubPool(t, engine)
		failure := errors.New("category page failed")
		engine.keyPageFailureOn = true
		engine.keyPageError = failure
		if err := pool.pruneKnownNewsCategories(t.Context()); !errors.Is(err, failure) {
			t.Fatalf("category page error = %v", err)
		}
	})
}
