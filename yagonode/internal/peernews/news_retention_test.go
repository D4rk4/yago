package peernews

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestNewsRecordWireSizeBoundary(t *testing.T) {
	record := Record{
		Originator: yagomodel.WordHash("peer"), Created: fixedNow(),
		Category: CategoryCrawlStart, Attributes: map[string]string{"x": ""},
	}
	record.Attributes["x"] = strings.Repeat(
		"x", maximumNewsRecordBytes-len(record.WireForm()),
	)
	wire := record.WireForm()
	if len(wire) != maximumNewsRecordBytes {
		t.Fatalf("wire size = %d", len(wire))
	}
	if _, err := parseRecord(wire, time.Time{}); err != nil {
		t.Fatalf("exact limit rejected: %v", err)
	}
	if _, err := parseRecord(wire+"x", time.Time{}); err == nil {
		t.Fatal("record above wire limit was accepted")
	}
}

func TestRetainedNewsTailNeverExceedsRecordOrByteBounds(t *testing.T) {
	tail := newBoundedNewestNews(8, 8*32)
	base := fixedNow()
	for index := range 10_000 {
		tail.Add(retainedNewsRecord{
			key:     vault.Key(fmt.Sprintf("%08d", index)),
			created: base.Add(time.Duration(index) * time.Second),
			bytes:   32,
		})
	}
	if tail.records.Len() != 8 || len(tail.keys) != 8 || cap(tail.records) != 8 ||
		tail.bytes != 8*32 {
		t.Fatalf(
			"tail = records %d keys %d capacity %d bytes %d",
			tail.records.Len(), len(tail.keys), cap(tail.records), tail.bytes,
		)
	}
}

func TestNewsRetentionExpiresDefaultAndExtendedCategoriesAcrossRestart(t *testing.T) {
	clock := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	engine := newNewsStubEngine()
	pool := openStubPoolAt(t, engine, func() time.Time { return clock })
	normal := retentionRecord(clock, 1, CategoryBookmarkAdd)
	extended := retentionRecord(clock, 2, CategoryCrawlStart)
	for _, record := range []Record{normal, extended} {
		stored, err := pool.EnqueueIncomingNews(t.Context(), record)
		if err != nil || !stored {
			t.Fatalf("enqueue %s = %t/%v", record.Category, stored, err)
		}
	}
	if err := pool.vault.Close(); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(25 * time.Hour)
	pool = openStubPoolAt(t, engine, func() time.Time { return clock })
	if _, found, err := pool.ByID(t.Context(), Incoming, normal.ID()); err != nil || found {
		t.Fatalf("default record after 25h = %t/%v", found, err)
	}
	if _, found, err := pool.ByID(t.Context(), Incoming, extended.ID()); err != nil || !found {
		t.Fatalf("extended record after 25h = %t/%v", found, err)
	}
	if stored, err := pool.EnqueueIncomingNews(t.Context(), extended); err != nil || stored {
		t.Fatalf("restart duplicate = %t/%v", stored, err)
	}
	if err := pool.vault.Close(); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(48 * time.Hour)
	pool = openStubPoolAt(t, engine, func() time.Time { return clock })
	t.Cleanup(func() { _ = pool.vault.Close() })
	if _, found, err := pool.ByID(t.Context(), Incoming, extended.ID()); err != nil || found {
		t.Fatalf("extended record after 73h = %t/%v", found, err)
	}
}

func TestNewsStartupMigratesLegacyKnownMarkersBeforeApplyingExactLifetime(t *testing.T) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	created := fixedNow().Add(-25 * time.Hour)
	extended := retentionRecord(created, 0, CategoryCrawlStart)
	short := retentionRecord(created, 1, CategoryBookmarkAdd)
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		for sequence, record := range []Record{extended, short} {
			if err := pool.known.Put(tx, vault.Key(record.ID()), knownMarker); err != nil {
				return fmt.Errorf("put legacy membership: %w", err)
			}
			if err := pool.queue.Put(
				tx, queueKey(Incoming, uint64(sequence+1)), record.WireForm(),
			); err != nil {
				return fmt.Errorf("put legacy queue record: %w", err)
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := pool.prune(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, found := engine.buckets[queueBucket][string(queueKey(Incoming, 1))]; !found {
		t.Fatal("extended legacy queue record was deleted")
	}
	if _, found := engine.buckets[knownBucket][extended.ID()]; !found {
		t.Fatal("extended legacy known marker was deleted")
	}
	if got := string(engine.buckets[knownBucket][extended.ID()]); got != knownMarker {
		t.Fatalf("migrated known marker = %q", got)
	}
	if got := storedKnownCategory(t, engine, extended.ID()); got != extended.Category {
		t.Fatalf("migrated known category = %q", got)
	}
	if _, found := engine.buckets[queueBucket][string(queueKey(Incoming, 2))]; found {
		t.Fatal("expired default legacy queue record was retained")
	}
	if _, found := engine.buckets[knownBucket][short.ID()]; found {
		t.Fatal("expired default legacy known marker was retained")
	}
}

func TestNewsStartupDropsMismatchedQueueRowWithoutOrphaningMatchingRow(t *testing.T) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	matching := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
	mismatched := matching
	mismatched.Category = CategoryBookmarkAdd
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := putKnownNewsFixture(pool, tx, matching); err != nil {
			return fmt.Errorf("put matching membership: %w", err)
		}
		if err := pool.queue.Put(
			tx, queueKey(Incoming, 1), matching.WireForm(),
		); err != nil {
			return fmt.Errorf("put matching queue record: %w", err)
		}

		return pool.queue.Put(tx, queueKey(Incoming, 2), mismatched.WireForm())
	}); err != nil {
		t.Fatal(err)
	}
	if err := pool.prune(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, found := engine.buckets[knownBucket][matching.ID()]; !found {
		t.Fatal("matching known identity was deleted")
	}
	if _, found := engine.buckets[queueBucket][string(queueKey(Incoming, 1))]; !found {
		t.Fatal("matching queue row was deleted")
	}
	if _, found := engine.buckets[queueBucket][string(queueKey(Incoming, 2))]; found {
		t.Fatal("mismatched queue row was retained")
	}
}

func TestNewsRetentionPrunesOverCapLegacyRowsWithBoundedSelection(t *testing.T) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	pool.retention = newsRetention{
		queueRecords: 8,
		queueBytes:   maximumNewsQueueBytes,
		knownRecords: 8,
	}
	base := fixedNow()
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		for index := range 1_000 {
			record := retentionRecord(base.Add(-time.Hour), index, CategoryCrawlStart)
			if err := putKnownNewsFixture(pool, tx, record); err != nil {
				return fmt.Errorf("put bounded membership: %w", err)
			}
			if err := pool.queue.Put(
				tx,
				queueKey(Incoming, uint64(index+1)),
				record.WireForm(),
			); err != nil {
				return fmt.Errorf("put bounded queue record: %w", err)
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	engine.pageAfter = nil
	engine.pageLimits = nil
	if err := pool.prune(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(engine.buckets[knownBucket]) != 8 || len(engine.buckets[queueBucket]) != 8 {
		t.Fatalf(
			"retained buckets = %d/%d, want 8/8",
			len(engine.buckets[knownBucket]), len(engine.buckets[queueBucket]),
		)
	}
	for _, limit := range engine.pageLimits {
		if limit != newsScrubPage {
			t.Fatalf("page limit = %d, want %d", limit, newsScrubPage)
		}
	}
}

func TestNewsPoolConcurrentIntakeKeepsNewestBoundedRecords(t *testing.T) {
	pool := openMemPool(t)
	pool.retention = newsRetention{
		queueRecords: 16,
		queueBytes:   maximumNewsQueueBytes,
		knownRecords: 16,
	}
	base := fixedNow().Add(-time.Hour)
	var group sync.WaitGroup
	for index := range 64 {
		group.Add(1)
		go func() {
			defer group.Done()
			record := retentionRecord(base, index, CategoryCrawlStart)
			if _, err := pool.EnqueueIncomingNews(t.Context(), record); err != nil {
				t.Errorf("enqueue %d: %v", index, err)
			}
		}()
	}
	group.Wait()
	recent, err := pool.Recent(t.Context(), Incoming, 64)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(recent))
	for _, record := range recent {
		got = append(got, record.ID())
	}
	slices.Sort(got)
	want := make([]string, 0, 16)
	for index := 48; index < 64; index++ {
		want = append(want, retentionRecord(base, index, CategoryCrawlStart).ID())
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("retained IDs = %v, want %v", got, want)
	}
}

func retentionRecord(base time.Time, index int, category string) Record {
	return Record{
		Originator: yagomodel.WordHash("peer"),
		Created:    base.Add(time.Duration(index) * time.Second).UTC().Truncate(time.Second),
		Category:   category,
		Attributes: map[string]string{"entry": fmt.Sprintf("%04d", index)},
	}
}

func TestNewsRetentionSelectionHonorsCancellation(t *testing.T) {
	pool := openMemPool(t)
	if err := pool.writePermit.Acquire(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer pool.writePermit.Release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := pool.prune(ctx); err == nil {
		t.Fatal("cancelled retention scan succeeded")
	}
}

func TestNewsIntakeRejectsExpiredAndFarFutureCreation(t *testing.T) {
	base := fixedNow()
	tests := []struct {
		name    string
		created time.Time
		stored  bool
	}{
		{name: "expired", created: base.Add(-defaultNewsLifetime - time.Second)},
		{name: "future boundary", created: base.Add(maximumNewsFutureSkew), stored: true},
		{name: "far future", created: base.Add(maximumNewsFutureSkew + time.Second)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool := openMemPool(t)
			record := retentionRecord(test.created, 0, CategoryBookmarkAdd)
			stored, err := pool.EnqueueIncomingNews(t.Context(), record)
			if err != nil || stored != test.stored {
				t.Fatalf("stored = %t/%v, want %t", stored, err, test.stored)
			}
		})
	}
}

func TestNewsRetentionKeepsNewestCreatedRecordsAcrossOutOfOrderAdmission(t *testing.T) {
	pool := openMemPool(t)
	pool.retention = newsRetention{
		queueRecords: 2, queueBytes: maximumNewsQueueBytes, knownRecords: 2,
	}
	base := fixedNow().Add(-time.Hour)
	for _, index := range []int{3, 1, 2} {
		record := retentionRecord(base, index, CategoryCrawlStart)
		stored, err := pool.EnqueueIncomingNews(t.Context(), record)
		if err != nil || !stored {
			t.Fatalf("enqueue %d = %t/%v", index, stored, err)
		}
	}
	recent, err := pool.Recent(t.Context(), Incoming, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		retentionRecord(base, 3, CategoryCrawlStart).ID(),
		retentionRecord(base, 2, CategoryCrawlStart).ID(),
	}
	got := []string{recent[0].ID(), recent[1].ID()}
	if !slices.Equal(got, want) {
		t.Fatalf("retained IDs = %v, want %v", got, want)
	}
}

func TestNewsRetentionReportsOlderRecordAsNotStored(t *testing.T) {
	pool := openMemPool(t)
	pool.retention = newsRetention{
		queueRecords: 2, queueBytes: maximumNewsQueueBytes, knownRecords: 2,
	}
	base := fixedNow().Add(-time.Hour)
	for _, index := range []int{3, 2} {
		stored, err := pool.EnqueueIncomingNews(
			t.Context(), retentionRecord(base, index, CategoryCrawlStart),
		)
		if err != nil || !stored {
			t.Fatalf("enqueue %d = %t/%v", index, stored, err)
		}
	}
	older := retentionRecord(base, 1, CategoryCrawlStart)
	stored, err := pool.EnqueueIncomingNews(t.Context(), older)
	if err != nil || stored {
		t.Fatalf("older enqueue = %t/%v, want not stored", stored, err)
	}
	if _, found, err := pool.ByID(t.Context(), Incoming, older.ID()); err != nil || found {
		t.Fatalf("older record = %t/%v", found, err)
	}
}

func TestNewsStartupPruneDoesNotLetCorruptLateRowsDisplaceValidNews(t *testing.T) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	pool.retention = newsRetention{
		queueRecords: 8, queueBytes: maximumNewsQueueBytes, knownRecords: 8,
	}
	base := fixedNow().Add(-time.Hour)
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		for index := range 9 {
			record := retentionRecord(base, index, CategoryCrawlStart)
			if err := putKnownNewsFixture(pool, tx, record); err != nil {
				return fmt.Errorf("put valid known news: %w", err)
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	engine.buckets[knownBucket]["zzzzzzzzzzzzzzzzzzzzzzzzzz"] = []byte("corrupt")
	if err := pool.prune(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(engine.buckets[knownBucket]) != 8 {
		t.Fatalf("known rows = %d, want 8", len(engine.buckets[knownBucket]))
	}
	oldest := retentionRecord(base, 0, CategoryCrawlStart).ID()
	if _, found := engine.buckets[knownBucket][oldest]; found {
		t.Fatalf("oldest valid row %q was retained", oldest)
	}
	for index := 1; index < 9; index++ {
		id := retentionRecord(base, index, CategoryCrawlStart).ID()
		if _, found := engine.buckets[knownBucket][id]; !found {
			t.Fatalf("new valid row %q was discarded", id)
		}
	}
}

func TestNewsStartupRejectsOversizedAndUnknownQueueRowsWithoutReadingValues(t *testing.T) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := putKnownNewsFixture(pool, tx, record); err != nil {
			return fmt.Errorf("put oversized queue membership: %w", err)
		}
		if err := pool.queue.Put(
			tx, queueKey(Incoming, 1), strings.Repeat("x", maximumNewsRecordBytes+1),
		); err != nil {
			return fmt.Errorf("put oversized queue record: %w", err)
		}

		return pool.queue.Put(tx, vault.Key("unknown/12345678"), record.WireForm())
	}); err != nil {
		t.Fatal(err)
	}
	engine.getCalls[queueBucket] = 0
	if err := pool.prune(t.Context()); err != nil {
		t.Fatal(err)
	}
	if engine.getCalls[queueBucket] != 0 {
		t.Fatalf("queue value reads = %d, want 0", engine.getCalls[queueBucket])
	}
	if len(engine.buckets[queueBucket]) != 0 {
		t.Fatalf("queue rows = %d, want 0", len(engine.buckets[queueBucket]))
	}
}

func TestNewsCleanupCommitsProgressBeforeLegacyScanCompletes(t *testing.T) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	pool.retention = newsRetention{
		queueRecords: 64, queueBytes: maximumNewsQueueBytes, knownRecords: 64,
	}
	base := fixedNow().Add(-time.Hour)
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		for index := range newsScrubPage + 512 {
			record := retentionRecord(base, index, CategoryCrawlStart)
			if err := putKnownNewsFixture(pool, tx, record); err != nil {
				return fmt.Errorf("put cleanup membership: %w", err)
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	engine.keyPageReads = 0
	engine.keyPageLimit = 1
	engine.keyPageError = errors.New("scan budget exhausted")
	if err := pool.prune(t.Context()); err == nil {
		t.Fatal("bounded startup scan unexpectedly completed")
	}
	remaining := len(engine.buckets[knownBucket])
	if remaining >= newsScrubPage+512 || remaining < pool.retention.knownRecords {
		t.Fatalf("known rows after interrupted cleanup = %d", remaining)
	}
	engine.keyPageLimit = 0
	engine.keyPageReads = 0
	if err := pool.prune(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(engine.buckets[knownBucket]) != pool.retention.knownRecords {
		t.Fatalf("known rows after resumed cleanup = %d", len(engine.buckets[knownBucket]))
	}
}

func TestKnownNewsEvictionHidesAndSkipsOrphanedQueueRows(t *testing.T) {
	pool := openMemPool(t)
	pool.retention = newsRetention{
		queueRecords: 8, queueBytes: maximumNewsQueueBytes, knownRecords: 1,
	}
	originator := yagomodel.WordHash("peer")
	if err := pool.PublishOwnNews(
		t.Context(), originator, CategoryCrawlStart, map[string]string{attributeIDOffset: "-2"},
	); err != nil {
		t.Fatal(err)
	}
	if err := pool.PublishOwnNews(
		t.Context(), originator, CategoryCrawlStart, map[string]string{attributeIDOffset: "-1"},
	); err != nil {
		t.Fatal(err)
	}
	recent, err := pool.Recent(t.Context(), Incoming, 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 || !recent[0].Created.Equal(fixedNow().Add(-time.Second)) {
		t.Fatalf("visible incoming news = %#v", recent)
	}
	published, found, err := pool.NextPublication(t.Context())
	if err != nil || !found || !published.Created.Equal(fixedNow().Add(-time.Second)) {
		t.Fatalf("next publication = %#v/%t/%v", published, found, err)
	}
}

func TestKnownNewsCategoryMismatchHidesAndSkipsQueueRows(t *testing.T) {
	pool := openMemPool(t)
	originator := yagomodel.WordHash("peer")
	if err := pool.PublishOwnNews(
		t.Context(), originator, CategoryCrawlStart, map[string]string{attributeIDOffset: "-2"},
	); err != nil {
		t.Fatal(err)
	}
	if err := pool.PublishOwnNews(
		t.Context(), originator, CategoryCrawlStart, map[string]string{attributeIDOffset: "-1"},
	); err != nil {
		t.Fatal(err)
	}
	first := retentionRecord(fixedNow(), -2, CategoryCrawlStart)
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return pool.knownCategories.Put(
			tx, vault.Key(first.ID()), CategoryBookmarkAdd,
		)
	}); err != nil {
		t.Fatal(err)
	}
	recent, err := pool.Recent(t.Context(), Incoming, 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 || !recent[0].Created.Equal(fixedNow().Add(-time.Second)) {
		t.Fatalf("visible incoming news = %#v", recent)
	}
	published, found, err := pool.NextPublication(t.Context())
	if err != nil || !found || !published.Created.Equal(fixedNow().Add(-time.Second)) {
		t.Fatalf("next publication = %#v/%t/%v", published, found, err)
	}
}

func TestNewsWritePermitHonorsCancellation(t *testing.T) {
	pool := openMemPool(t)
	if err := pool.writePermit.Acquire(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer pool.writePermit.Release()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := pool.EnqueueIncomingNews(ctx, retentionRecord(
		fixedNow().Add(-time.Hour), 0, CategoryCrawlStart,
	))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("enqueue error = %v, want context cancellation", err)
	}
}

func TestNewsIntakeHotPathDoesNotScanRetentionBuckets(t *testing.T) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	sentinel := errors.New("unexpected retention scan")
	engine.scanErrors[knownBucket] = sentinel
	engine.scanErrors[queueBucket] = sentinel
	base := fixedNow().Add(-time.Hour)
	for index := range 256 {
		stored, err := pool.EnqueueIncomingNews(
			t.Context(), retentionRecord(base, index, CategoryCrawlStart),
		)
		if err != nil || !stored {
			t.Fatalf("enqueue %d = %t/%v", index, stored, err)
		}
	}
}
