package peernews

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestKnownCategoryEvidenceStorageBoundaries(t *testing.T) {
	failure := errors.New("known category evidence failed")
	for _, test := range []struct {
		name     string
		exercise func(*testing.T, error)
	}{
		{name: "lookup read", exercise: newsCategoryLookupReadFailure},
		{name: "missing value", exercise: newsCategoryMissingValue},
		{name: "malformed generation", exercise: newsCategoryMalformedGeneration},
		{name: "replace", exercise: newsCategoryReplacement},
		{name: "replace failure", exercise: newsCategoryReplacementFailure},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.exercise(t, failure)
		})
	}
}

func newsCategoryLookupReadFailure(t *testing.T, failure error) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	key := vault.Key("identity")
	engine.buckets[knownCategoryBucket][string(key)] = []byte(CategoryCrawlStart)
	engine.readErrors[knownCategoryBucket] = failure
	err := pool.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, _, err := pool.knownNewsCategory(tx, key)

		return err
	})
	if !errors.Is(err, failure) {
		t.Fatalf("category lookup error = %v", err)
	}
}

func newsCategoryMissingValue(t *testing.T, _ error) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	key := vault.Key("identity")
	engine.buckets[knownCategoryBucket][string(key)] = []byte(CategoryCrawlStart)
	engine.missingReads[knownCategoryBucket] = map[string]bool{string(key): true}
	err := pool.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, _, err := pool.storedKnownCategoryEvidence(tx, key)

		return err
	})
	if !errors.Is(err, vault.ErrCorruptValue) {
		t.Fatalf("missing category error = %v", err)
	}
}

func newsCategoryMalformedGeneration(t *testing.T, _ error) {
	codec := knownCategoryCodec{}
	if _, err := codec.Encode(CategoryCrawlStart + "\x00invalid"); err == nil {
		t.Fatal("malformed category generation was accepted")
	}
}

func newsCategoryReplacement(t *testing.T, _ error) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	key := vault.Key("identity")
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return pool.replaceKnownNewsCategory(tx, key, CategoryCrawlStart)
	}); err != nil {
		t.Fatal(err)
	}
	if got := string(engine.buckets[knownCategoryBucket][string(key)]); got != CategoryCrawlStart {
		t.Fatalf("stored category = %q", got)
	}
}

func newsCategoryReplacementFailure(t *testing.T, failure error) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	engine.putErrors[knownCategoryBucket] = failure
	err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return pool.replaceKnownNewsCategory(tx, vault.Key("identity"), CategoryCrawlStart)
	})
	if !errors.Is(err, failure) {
		t.Fatalf("category replacement error = %v", err)
	}
}

func TestKnownCategoryRetentionPreservesOperationalMarkerFailure(t *testing.T) {
	failure := errors.New("known marker inspection failed")
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	key := vault.Key("identity")
	engine.buckets[knownBucket][string(key)] = []byte(knownMarker)
	engine.buckets[knownCategoryBucket][string(key)] = []byte(CategoryCrawlStart)
	want := cloneNewsBuckets(engine.buckets)
	engine.valueSizeErrors[knownBucket] = failure
	err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return pool.pruneKnownNewsCategoryRecord(tx, key)
	})
	if !errors.Is(err, failure) {
		t.Fatalf("category retention error = %v", err)
	}
	if !reflect.DeepEqual(engine.buckets, want) {
		t.Fatalf(
			"category retention mutated durable state\ngot:  %#v\nwant: %#v",
			engine.buckets,
			want,
		)
	}
}

func TestKnownCategoryCleanupRecoveryBoundaries(t *testing.T) {
	failure := errors.New("known category cleanup failed")
	for _, test := range []struct {
		name     string
		exercise func(*testing.T, error)
	}{
		{name: "cursor read", exercise: newsCategoryCleanupCursorFailure},
		{name: "prefix read", exercise: newsCategoryCleanupPrefixFailure},
		{name: "stale reset failure", exercise: newsCategoryCleanupResetFailure},
		{name: "stale reset", exercise: newsCategoryCleanupReset},
		{name: "checkpoint", exercise: newsCategoryCleanupCheckpointFailure},
		{name: "multiple pages", exercise: newsCategoryCleanupMultiplePages},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.exercise(t, failure)
		})
	}
}

func newsCategoryCleanupCursorFailure(t *testing.T, failure error) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	engine.buckets[cleanupBucket][string(categoryCleanupCursorKey)] = []byte("identity")
	engine.readErrors[cleanupBucket] = failure
	if err := pool.pruneKnownNewsCategories(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("category cursor error = %v", err)
	}
}

func newsCategoryCleanupPrefixFailure(t *testing.T, failure error) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	key := vault.Key("identity")
	engine.buckets[knownBucket][string(key)] = []byte(knownMarker)
	engine.buckets[knownCategoryBucket][string(key)] = []byte(CategoryCrawlStart)
	engine.buckets[cleanupBucket][string(categoryCleanupCursorKey)] = append([]byte(nil), key...)
	engine.readErrors[knownBucket] = failure
	if err := pool.pruneKnownNewsCategories(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("category prefix error = %v", err)
	}
}

func newsCategoryCleanupResetFailure(t *testing.T, failure error) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	key := vault.Key("orphan")
	engine.buckets[knownCategoryBucket][string(key)] = []byte(CategoryCrawlStart)
	engine.buckets[cleanupBucket][string(categoryCleanupCursorKey)] = append([]byte(nil), key...)
	engine.deleteErrors[cleanupBucket] = failure
	if err := pool.pruneKnownNewsCategories(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("category reset error = %v", err)
	}
}

func newsCategoryCleanupReset(t *testing.T, _ error) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	key := vault.Key("orphan")
	engine.buckets[knownCategoryBucket][string(key)] = []byte(CategoryCrawlStart)
	engine.buckets[cleanupBucket][string(categoryCleanupCursorKey)] = append([]byte(nil), key...)
	if err := pool.pruneKnownNewsCategories(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, found := engine.buckets[knownCategoryBucket][string(key)]; found {
		t.Fatal("stale category cleanup retained the orphan")
	}
}

func newsCategoryCleanupCheckpointFailure(t *testing.T, failure error) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	key := vault.Key("identity")
	engine.buckets[knownBucket][string(key)] = []byte(knownMarker)
	engine.buckets[knownCategoryBucket][string(key)] = []byte(CategoryCrawlStart)
	engine.putErrors[cleanupBucket] = failure
	if err := pool.pruneKnownNewsCategories(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("category checkpoint error = %v", err)
	}
}

func newsCategoryCleanupMultiplePages(t *testing.T, _ error) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	for index := range newsScrubPage + 1 {
		key := fmt.Sprintf("identity-%04d", index)
		engine.buckets[knownBucket][key] = []byte(knownMarker)
		engine.buckets[knownCategoryBucket][key] = []byte(CategoryCrawlStart)
	}
	engine.keyPageAfterByBucket[knownCategoryBucket] = nil
	if err := pool.pruneKnownNewsCategories(t.Context()); err != nil {
		t.Fatal(err)
	}
	after := engine.keyPageAfterByBucket[knownCategoryBucket]
	if len(after) != 2 || after[0] != "" || after[1] != "identity-1023" {
		t.Fatalf("category page cursors = %q", after)
	}
}

func TestRetainedKnownNewsPropagatesCategoryReadFailure(t *testing.T) {
	failure := errors.New("retained known category failed")
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
	key := vault.Key(record.ID())
	engine.buckets[knownBucket][string(key)] = []byte(knownMarker)
	engine.buckets[knownCategoryBucket][string(key)] = []byte(knownCategoryEvidence(record))
	engine.readErrors[knownCategoryBucket] = failure
	err := pool.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, _, err := pool.retainedKnownNewsRecord(tx, key, fixedNow())

		return err
	})
	if !errors.Is(err, failure) {
		t.Fatalf("retained known category error = %v", err)
	}
}

func TestKnownRecordMembershipStorageBoundaries(t *testing.T) {
	failure := errors.New("known membership failed")
	for _, test := range []struct {
		name     string
		exercise func(*testing.T, error)
	}{
		{name: "category read", exercise: newsMembershipCategoryReadFailure},
		{name: "corrupt legacy category", exercise: newsMembershipCorruptCategoryFallback},
		{name: "missing marker", exercise: newsMembershipMissingMarkerValue},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.exercise(t, failure)
		})
	}
}

func newsMembershipCategoryReadFailure(t *testing.T, failure error) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
	key := vault.Key(record.ID())
	engine.buckets[knownBucket][string(key)] = []byte(knownMarker)
	engine.buckets[knownCategoryBucket][string(key)] = []byte(knownCategoryEvidence(record))
	engine.readErrors[knownCategoryBucket] = failure
	err := pool.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, err := pool.knownRecordMatches(tx, record)

		return err
	})
	if !errors.Is(err, failure) {
		t.Fatalf("known membership error = %v", err)
	}
}

func newsMembershipCorruptCategoryFallback(t *testing.T, _ error) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	record := retentionRecord(fixedNow().Add(-time.Hour), 0, "")
	key := vault.Key(record.ID())
	engine.buckets[knownBucket][string(key)] = []byte(knownMarker)
	engine.buckets[knownCategoryBucket][string(key)] = []byte("invalid-category")
	matched := false
	err := pool.vault.View(t.Context(), func(tx *vault.Txn) error {
		var err error
		matched, err = pool.knownRecordMatches(tx, record)

		return err
	})
	if err != nil || !matched {
		t.Fatalf("legacy membership = %t/%v", matched, err)
	}
}

func newsMembershipMissingMarkerValue(t *testing.T, _ error) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	key := vault.Key("identity")
	engine.buckets[knownBucket][string(key)] = []byte(knownMarker)
	engine.missingReads[knownBucket] = map[string]bool{string(key): true}
	err := pool.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, err := pool.storedKnownMarkerPresent(tx, key)

		return err
	})
	if !errors.Is(err, vault.ErrCorruptValue) {
		t.Fatalf("missing marker error = %v", err)
	}
}

func TestQueuedNewsEvidencePriorityStorageBoundaries(t *testing.T) {
	failure := errors.New("queued evidence priority failed")
	for _, test := range []struct {
		name     string
		exercise func(*testing.T, error)
	}{
		{name: "marker read", exercise: queuedEvidenceMarkerReadFailure},
		{name: "missing row", exercise: queuedEvidenceMissingRow},
		{name: "corrupt category", exercise: queuedEvidenceCorruptCategory},
		{name: "category read", exercise: queuedEvidenceCategoryReadFailure},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.exercise(t, failure)
		})
	}
}

func queuedEvidenceMissingRow(t *testing.T, _ error) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	selected := true
	err := pool.vault.View(t.Context(), func(tx *vault.Txn) error {
		var candidateErr error
		_, _, selected, candidateErr = pool.queuedNewsEvidenceCandidate(
			tx,
			queueKey(Incoming, 1),
			fixedNow(),
		)

		return candidateErr
	})
	if err != nil || selected {
		t.Fatalf("missing evidence selected = %t/%v", selected, err)
	}
}

func queuedEvidenceMarkerReadFailure(t *testing.T, failure error) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
	engine.buckets[knownBucket][record.ID()] = []byte(knownMarker)
	engine.readErrors[knownBucket] = failure
	err := pool.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, _, err := pool.queuedNewsEvidencePriority(tx, record)

		return err
	})
	if !errors.Is(err, failure) {
		t.Fatalf("queued marker error = %v", err)
	}
}

func queuedEvidenceCorruptCategory(t *testing.T, _ error) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
	engine.buckets[knownBucket][record.ID()] = []byte(knownMarker)
	engine.buckets[knownCategoryBucket][record.ID()] = []byte("invalid-category")
	priority := -1
	selected := false
	err := pool.vault.View(t.Context(), func(tx *vault.Txn) error {
		var err error
		priority, selected, err = pool.queuedNewsEvidencePriority(tx, record)

		return err
	})
	if err != nil || !selected || priority != 0 {
		t.Fatalf("corrupt category priority = %d/%t/%v", priority, selected, err)
	}
}

func queuedEvidenceCategoryReadFailure(t *testing.T, failure error) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
	engine.buckets[knownBucket][record.ID()] = []byte(knownMarker)
	engine.buckets[knownCategoryBucket][record.ID()] = []byte(knownCategoryEvidence(record))
	engine.readErrors[knownCategoryBucket] = failure
	err := pool.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, _, err := pool.queuedNewsEvidencePriority(tx, record)

		return err
	})
	if !errors.Is(err, failure) {
		t.Fatalf("queued category error = %v", err)
	}
}

func TestQueuedNewsEvidenceCatalogHonorsIdentityCapacity(t *testing.T) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	pool.retention.knownRecords = 1
	records := []Record{
		retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart),
		retentionRecord(fixedNow().Add(-time.Hour), 1, CategoryCrawlStart),
	}
	for index, record := range records {
		engine.buckets[knownBucket][record.ID()] = []byte(knownMarker)
		engine.buckets[knownCategoryBucket][record.ID()] = []byte(knownCategoryEvidence(record))
		engine.buckets[queueBucket][string(queueKey(Incoming, uint64(index+1)))] = []byte(
			record.WireForm(),
		)
	}
	catalog, err := pool.buildQueuedNewsEvidenceCatalog(t.Context(), fixedNow())
	if err != nil {
		t.Fatal(err)
	}
	_, firstFound := catalog.evidence[records[0].ID()]
	_, secondFound := catalog.evidence[records[1].ID()]
	if len(catalog.evidence) != 1 || !firstFound || secondFound ||
		catalog.latestSequence[Incoming] != 2 {
		t.Fatalf(
			"bounded catalog = %d entries, first=%t second=%t sequence=%d",
			len(catalog.evidence),
			firstFound,
			secondFound,
			catalog.latestSequence[Incoming],
		)
	}
}

func TestQueuedNewsCursorWriteFailuresAreReported(t *testing.T) {
	failure := errors.New("queued cursor write failed")
	for _, test := range []struct {
		name string
		raw  string
	}{
		{name: "repair corrupt", raw: "123456789012345678901"},
		{name: "raise floor", raw: "1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := newNewsStubEngine()
			pool := openStubPool(t, engine)
			engine.buckets[cursorBucket][string(Incoming)] = []byte(test.raw)
			engine.putErrors[cursorBucket] = failure
			if err := pool.raiseQueuedNewsCursors(
				t.Context(), map[Queue]uint64{Incoming: 7},
			); !errors.Is(err, failure) {
				t.Fatalf("cursor update error = %v", err)
			}
		})
	}
}
