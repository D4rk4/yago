package peernews

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestNewsRecoveryIntentsAdmitMaximumEscapableRecords(t *testing.T) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	record := maximumEscapableNewsRecord(10)
	if len(record.WireForm()) != maximumNewsRecordBytes {
		t.Fatalf("fixture wire size = %d", len(record.WireForm()))
	}
	admission := newsAdmission{record: record, destinations: []Queue{Incoming}}
	if err := pool.storeNewsAdmission(t.Context(), admission); err != nil {
		t.Fatal(err)
	}
	if size := len(engine.buckets[cleanupBucket][string(newsAdmissionKey)]); size == 0 ||
		size > maximumNewsCleanupValueBytes {
		t.Fatalf("admission intent size = %d", size)
	}
	decodedAdmission, found, err := pool.readNewsAdmission(t.Context())
	if err != nil || !found || decodedAdmission.record.WireForm() != record.WireForm() {
		t.Fatalf("decoded admission = %#v/%t/%v", decodedAdmission, found, err)
	}
	if err := pool.clearNewsAdmission(t.Context()); err != nil {
		t.Fatal(err)
	}

	rotation := newsRotation{
		source: queueKey(Outgoing, 1), original: record,
		rotated: incrementedNewsRecord(record), destination: Outgoing,
	}
	if err := pool.storeNewsRotation(t.Context(), rotation); err != nil {
		t.Fatal(err)
	}
	if size := len(engine.buckets[cleanupBucket][string(newsRotationKey)]); size == 0 ||
		size > maximumNewsCleanupValueBytes {
		t.Fatalf("rotation intent size = %d", size)
	}
	decodedRotation, found, err := pool.readNewsRotation(t.Context())
	if err != nil || !found || decodedRotation.original.WireForm() != record.WireForm() {
		t.Fatalf("decoded rotation = %#v/%t/%v", decodedRotation, found, err)
	}
	if err := pool.clearNewsRotation(t.Context()); err != nil {
		t.Fatal(err)
	}

	stored, err := pool.storeNewsRecord(
		t.Context(), record, fixedNow(), []Queue{Outgoing},
	)
	if err != nil || !stored {
		t.Fatalf("store maximum publication = %t/%v", stored, err)
	}
	published, found, err := pool.NextPublication(t.Context())
	if err != nil || !found || published.Distributed != 11 ||
		len(published.WireForm()) != maximumNewsRecordBytes {
		t.Fatalf("maximum publication = %#v/%t/%v", published, found, err)
	}
}

func TestQueuedNewsCursorReconciliationPreventsOverwrite(t *testing.T) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	first := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := putKnownNewsFixture(pool, tx, first); err != nil {
			return err
		}
		if err := pool.queue.Put(tx, queueKey(Incoming, 10), first.WireForm()); err != nil {
			return fmt.Errorf("store queued news fixture: %w", err)
		}

		if err := pool.cursor.Put(tx, vault.Key(Incoming), 1); err != nil {
			return fmt.Errorf("store news cursor fixture: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := pool.pruneQueuedNews(t.Context(), fixedNow()); err != nil {
		t.Fatal(err)
	}
	second := retentionRecord(fixedNow().Add(-time.Hour), 1, CategoryCrawlStart)
	stored, err := pool.EnqueueIncomingNews(t.Context(), second)
	if err != nil || !stored {
		t.Fatalf("enqueue after cursor repair = %t/%v", stored, err)
	}
	if _, found := engine.buckets[queueBucket][string(queueKey(Incoming, 10))]; !found {
		t.Fatal("existing queue row was overwritten")
	}
	if _, found := engine.buckets[queueBucket][string(queueKey(Incoming, 11))]; !found {
		t.Fatal("new queue row did not use the reconciled sequence")
	}
}

func TestQueuedNewsCursorRepairAndExhaustion(t *testing.T) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	engine.buckets[cursorBucket][string(Incoming)] = []byte("corrupt")
	if err := pool.raiseQueuedNewsCursors(
		t.Context(), map[Queue]uint64{Incoming: 7},
	); err != nil {
		t.Fatal(err)
	}
	if got := string(engine.buckets[cursorBucket][string(Incoming)]); got != "7" {
		t.Fatalf("repaired cursor = %q", got)
	}
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return pool.cursor.Put(tx, vault.Key(Outgoing), math.MaxUint64)
	}); err != nil {
		t.Fatal(err)
	}
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		_, err := pool.push(tx, Outgoing, retentionRecord(
			fixedNow().Add(-time.Hour), 2, CategoryCrawlStart,
		))

		return err
	}); err == nil {
		t.Fatal("exhausted cursor wrapped")
	}
}

func TestNewsPublicationRejectsDistributionWidthOverflow(t *testing.T) {
	record := maximumEscapableNewsRecord(9)
	if len(record.WireForm()) != maximumNewsRecordBytes || newsPublicationAdmitted(record) {
		t.Fatalf(
			"overflow fixture = %d bytes, publishable=%t",
			len(record.WireForm()), newsPublicationAdmitted(record),
		)
	}
	pool := openMemPool(t)
	stored, err := pool.storeNewsRecord(
		t.Context(), record, fixedNow(), []Queue{Outgoing},
	)
	if err != nil || !stored {
		t.Fatalf("store legacy outgoing row = %t/%v", stored, err)
	}
	if published, found, err := pool.NextPublication(t.Context()); err != nil || found {
		t.Fatalf("oversized rotation = %#v/%t/%v", published, found, err)
	}
	if _, found, err := pool.ByID(t.Context(), Outgoing, record.ID()); err != nil || found {
		t.Fatalf("oversized outgoing row = %t/%v", found, err)
	}

	attributes := map[string]string{"x": record.Attributes["x"]}
	if err := pool.PublishOwnNews(
		t.Context(), record.Originator, record.Category, attributes,
	); err == nil {
		t.Fatal("new publication without distribution-width reserve was accepted")
	}
}

func maximumEscapableNewsRecord(distributed int) Record {
	record := Record{
		Originator:  yagomodel.WordHash("escape-news"),
		Created:     fixedNow().Add(-time.Hour),
		Category:    CategoryCrawlStart,
		Distributed: distributed,
		Attributes:  map[string]string{"x": ""},
	}
	remaining := maximumNewsRecordBytes - len(record.WireForm())
	pattern := "<&\"\\"
	record.Attributes["x"] = strings.Repeat(pattern, remaining/len(pattern)) +
		strings.Repeat("<", remaining%len(pattern))

	return record
}

func incrementedNewsRecord(record Record) Record {
	record.Distributed++

	return record
}
