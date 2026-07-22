package peernews

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type v0020KnownCodec struct{}

func (v0020KnownCodec) Encode(value string) ([]byte, error) { return []byte(value), nil }

func (v0020KnownCodec) Decode(raw []byte) (string, error) {
	if string(raw) != knownMarker {
		return "", fmt.Errorf("%w: known news marker %q", ErrBadNewsRecord, raw)
	}

	return knownMarker, nil
}

type v0020NewsPool struct {
	queue  *vault.Collection[string]
	known  *vault.Collection[string]
	cursor *vault.Collection[uint64]
	vault  *vault.Vault
}

func openV0020NewsPool(t *testing.T, engine *newsStubEngine) *v0020NewsPool {
	t.Helper()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("v0.0.20 vault: %v", err)
	}
	queue, err := vault.Register(storage, queueBucket, wireCodec{})
	if err != nil {
		t.Fatalf("v0.0.20 queue: %v", err)
	}
	known, err := vault.Register(storage, knownBucket, v0020KnownCodec{})
	if err != nil {
		t.Fatalf("v0.0.20 known: %v", err)
	}
	cursor, err := vault.Register(storage, cursorBucket, cursorCodec{})
	if err != nil {
		t.Fatalf("v0.0.20 cursor: %v", err)
	}

	return &v0020NewsPool{queue: queue, known: known, cursor: cursor, vault: storage}
}

func (p *v0020NewsPool) enqueueIncomingNews(
	ctx context.Context,
	record Record,
) (bool, error) {
	if record.Created.IsZero() || !knownNewsCategories[record.Category] {
		return false, nil
	}
	stored := false
	err := p.vault.Update(ctx, func(tx *vault.Txn) error {
		stored = false
		_, exists, err := p.known.Get(tx, vault.Key(record.ID()))
		if err != nil {
			return fmt.Errorf("check known news: %w", err)
		}
		if exists {
			return nil
		}
		if err := p.known.Put(tx, vault.Key(record.ID()), knownMarker); err != nil {
			return fmt.Errorf("remember news: %w", err)
		}
		stored = true
		sequence, _, err := p.cursor.Get(tx, vault.Key(Incoming))
		if err != nil {
			return fmt.Errorf("read incoming news cursor: %w", err)
		}
		sequence++
		if err := p.cursor.Put(tx, vault.Key(Incoming), sequence); err != nil {
			return fmt.Errorf("advance incoming news cursor: %w", err)
		}

		return p.queue.Put(tx, queueKey(Incoming, sequence), record.WireForm())
	})
	if err != nil {
		return false, fmt.Errorf("v0.0.20 enqueue incoming news: %w", err)
	}

	return stored, nil
}

func putKnownNewsFixture(pool *Pool, tx *vault.Txn, record Record) error {
	if err := pool.known.Put(tx, vault.Key(record.ID()), knownMarker); err != nil {
		return fmt.Errorf("put known news fixture: %w", err)
	}

	return pool.replaceKnownNewsCategoryForRecord(tx, vault.Key(record.ID()), record)
}

func TestKnownNewsPrimaryRowsRemainV0020CompatibleAcrossDowngrade(t *testing.T) {
	engine := newNewsStubEngine()
	current := openStubPool(t, engine)
	currentRecord := retentionRecord(
		fixedNow().Add(-time.Hour), 0, CategoryCrawlStart,
	)
	if stored, err := current.EnqueueIncomingNews(
		t.Context(), currentRecord,
	); err != nil || !stored {
		t.Fatalf("current enqueue = %t/%v", stored, err)
	}
	assertV0020KnownMarker(t, engine, currentRecord.ID())
	if got := storedKnownCategory(t, engine, currentRecord.ID()); got != currentRecord.Category {
		t.Fatalf("current category = %q", got)
	}

	legacy := openV0020NewsPool(t, engine)
	if stored, err := legacy.enqueueIncomingNews(
		t.Context(), currentRecord,
	); err != nil || stored {
		t.Fatalf("v0.0.20 duplicate = %t/%v", stored, err)
	}
	legacyRecord := retentionRecord(
		fixedNow().Add(-time.Hour), 1, CategoryBookmarkAdd,
	)
	if stored, err := legacy.enqueueIncomingNews(
		t.Context(), legacyRecord,
	); err != nil || !stored {
		t.Fatalf("v0.0.20 enqueue = %t/%v", stored, err)
	}
	assertV0020KnownMarker(t, engine, legacyRecord.ID())
	if _, found := engine.buckets[knownCategoryBucket][legacyRecord.ID()]; found {
		t.Fatal("v0.0.20 unexpectedly wrote category evidence")
	}

	upgraded := openStubPool(t, engine)
	if _, found, err := upgraded.ByID(
		t.Context(), Incoming, legacyRecord.ID(),
	); err != nil || !found {
		t.Fatalf("upgraded legacy row = %t/%v", found, err)
	}
	for _, record := range []Record{currentRecord, legacyRecord} {
		assertV0020KnownMarker(t, engine, record.ID())
		if got := storedKnownCategory(t, engine, record.ID()); got != record.Category {
			t.Fatalf("upgraded category for %s = %q", record.ID(), got)
		}
	}
	if _, err := (v0020KnownCodec{}).Decode(
		[]byte("c:" + currentRecord.Category),
	); err == nil {
		t.Fatal("v0.0.20 codec accepted an in-band category")
	}
	if _, err := (knownCodec{}).Encode("c:" + currentRecord.Category); err == nil {
		t.Fatal("current codec encoded an in-band category")
	}
}

func assertV0020KnownMarker(t *testing.T, engine *newsStubEngine, id string) {
	t.Helper()
	raw, found := engine.buckets[knownBucket][id]
	if !found || !bytes.Equal(raw, []byte(knownMarker)) {
		t.Fatalf("primary known row %s = %q/%t", id, raw, found)
	}
	if decoded, err := (v0020KnownCodec{}).Decode(raw); err != nil ||
		decoded != knownMarker {
		t.Fatalf("v0.0.20 decode %s = %q/%v", id, decoded, err)
	}
}

func TestKnownNewsCategoryScrubRepairsPartialAndCorruptState(t *testing.T) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	missing := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
	corrupt := retentionRecord(fixedNow().Add(-time.Hour), 1, CategoryBookmarkAdd)
	orphan := retentionRecord(fixedNow().Add(-time.Hour), 2, CategoryCrawlStart)
	withoutQueue := retentionRecord(fixedNow().Add(-time.Hour), 3, CategoryCrawlStart)
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		for sequence, record := range []Record{missing, corrupt} {
			if err := pool.known.Put(tx, vault.Key(record.ID()), knownMarker); err != nil {
				return fmt.Errorf("put partial known news: %w", err)
			}
			if err := pool.queue.Put(
				tx, queueKey(Incoming, uint64(sequence+1)), record.WireForm(),
			); err != nil {
				return fmt.Errorf("put partial queued news: %w", err)
			}
		}

		return pool.known.Put(tx, vault.Key(withoutQueue.ID()), knownMarker)
	}); err != nil {
		t.Fatal(err)
	}
	engine.buckets[knownCategoryBucket][corrupt.ID()] = []byte("too-long-category")
	engine.buckets[knownCategoryBucket][orphan.ID()] = []byte(orphan.Category)
	engine.buckets[knownCategoryBucket][withoutQueue.ID()] = []byte("corrupt-category")

	if err := pool.prune(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, record := range []Record{missing, corrupt} {
		assertV0020KnownMarker(t, engine, record.ID())
		if got := storedKnownCategory(t, engine, record.ID()); got != record.Category {
			t.Fatalf("repaired category for %s = %q", record.ID(), got)
		}
	}
	if _, found := engine.buckets[knownCategoryBucket][orphan.ID()]; found {
		t.Fatal("orphan category survived scrub")
	}
	assertV0020KnownMarker(t, engine, withoutQueue.ID())
	if _, found := engine.buckets[knownCategoryBucket][withoutQueue.ID()]; found {
		t.Fatal("corrupt category without queue survived scrub")
	}
}

func TestKnownNewsCategoryRetryReplacesOrphanEvidence(t *testing.T) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	record := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
	engine.buckets[knownCategoryBucket][record.ID()] = []byte(CategoryBookmarkAdd)
	stored, err := pool.EnqueueIncomingNews(t.Context(), record)
	if err != nil || !stored {
		t.Fatalf("retry over orphan category = %t/%v", stored, err)
	}
	assertV0020KnownMarker(t, engine, record.ID())
	if got := storedKnownCategory(t, engine, record.ID()); got != record.Category {
		t.Fatalf("replaced orphan category = %q", got)
	}
}

func TestDowngradeRewriteReplacesStaleGenerationBoundCategory(t *testing.T) {
	engine := newNewsStubEngine()
	current := openStubPool(t, engine)
	original := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
	if stored, err := current.EnqueueIncomingNews(t.Context(), original); err != nil || !stored {
		t.Fatalf("current enqueue = %t/%v", stored, err)
	}
	delete(engine.buckets[knownBucket], original.ID())
	for key, raw := range engine.buckets[queueBucket] {
		record, err := parseRecord(string(raw), time.Time{})
		if err == nil && record.ID() == original.ID() {
			delete(engine.buckets[queueBucket], key)
		}
	}

	rewritten := original
	rewritten.Category = CategoryBookmarkAdd
	legacy := openV0020NewsPool(t, engine)
	if stored, err := legacy.enqueueIncomingNews(t.Context(), rewritten); err != nil || !stored {
		t.Fatalf("downgrade rewrite = %t/%v", stored, err)
	}
	reopened := openStubPool(t, engine)
	recovered, found, err := reopened.ByID(t.Context(), Incoming, rewritten.ID())
	if err != nil || !found || recovered.Category != rewritten.Category {
		t.Fatalf("reopened rewrite = %#v/%t/%v", recovered, found, err)
	}
	if got := storedKnownCategory(t, engine, rewritten.ID()); got != rewritten.Category {
		t.Fatalf("reconciled category = %q, want %q", got, rewritten.Category)
	}
}

func TestDowngradeRewriteIgnoresExpiredStaleCategoryEvidence(t *testing.T) {
	clock := fixedNow()
	engine := newNewsStubEngine()
	original := retentionRecord(clock.Add(-25*time.Hour), 0, CategoryBookmarkAdd)
	current := openStubPoolAt(t, engine, func() time.Time {
		return original.Created.Add(time.Hour)
	})
	if stored, err := current.EnqueueIncomingNews(t.Context(), original); err != nil || !stored {
		t.Fatalf("current enqueue = %t/%v", stored, err)
	}
	for key, raw := range engine.buckets[queueBucket] {
		record, err := parseRecord(string(raw), time.Time{})
		if err == nil && record.ID() == original.ID() {
			delete(engine.buckets[queueBucket], key)
			engine.buckets[queueBucket][string(queueKey(Published, 1))] = raw
		}
	}
	delete(engine.buckets[knownBucket], original.ID())

	rewritten := original
	rewritten.Category = CategoryCrawlStart
	legacy := openV0020NewsPool(t, engine)
	if stored, err := legacy.enqueueIncomingNews(t.Context(), rewritten); err != nil || !stored {
		t.Fatalf("downgrade rewrite = %t/%v", stored, err)
	}
	reopened := openStubPoolAt(t, engine, func() time.Time { return clock })
	recovered, found, err := reopened.ByID(t.Context(), Incoming, rewritten.ID())
	if err != nil || !found || recovered.Category != rewritten.Category {
		t.Fatalf("reopened rewrite = %#v/%t/%v", recovered, found, err)
	}
	if _, found, err := reopened.ByID(t.Context(), Published, original.ID()); err != nil || found {
		t.Fatalf("expired stale row = %t/%v", found, err)
	}
}

type queuedNewsEvidencePreference struct {
	name       string
	evidence   string
	retained   Record
	retainedAt uint64
}

func TestQueuedNewsEvidencePrefersGenerationBindingOverStaleCategoryName(t *testing.T) {
	base := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
	categoryA := base
	categoryA.Attributes = map[string]string{"generation": "new-a"}
	categoryB := base
	categoryB.Category = CategoryBookmarkAdd
	categoryB.Attributes = map[string]string{"generation": "new-b"}
	staleA := base
	staleA.Attributes = map[string]string{"generation": "old-a"}

	for _, test := range []queuedNewsEvidencePreference{
		{
			name:       "stale bound category",
			evidence:   knownCategoryEvidence(staleA),
			retained:   categoryB,
			retainedAt: 1,
		},
		{
			name:       "legacy plain category",
			evidence:   categoryA.Category,
			retained:   categoryA,
			retainedAt: 2,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertQueuedNewsEvidencePreference(t, test, categoryA, categoryB)
		})
	}
}

func assertQueuedNewsEvidencePreference(
	t *testing.T,
	preference queuedNewsEvidencePreference,
	categoryA Record,
	categoryB Record,
) {
	t.Helper()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	id := vault.Key(categoryA.ID())
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := pool.known.Put(tx, id, knownMarker); err != nil {
			return fmt.Errorf("store known news marker: %w", err)
		}
		if err := pool.knownCategories.Put(tx, id, preference.evidence); err != nil {
			return fmt.Errorf("store known news evidence: %w", err)
		}
		if err := pool.queue.Put(
			tx,
			queueKey(Incoming, 1),
			categoryB.WireForm(),
		); err != nil {
			return fmt.Errorf("store competing queued news: %w", err)
		}
		if err := pool.queue.Put(
			tx,
			queueKey(Incoming, 2),
			categoryA.WireForm(),
		); err != nil {
			return fmt.Errorf("store preferred queued news: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := pool.pruneQueuedNews(t.Context(), fixedNow()); err != nil {
		t.Fatal(err)
	}
	if got := len(engine.buckets[queueBucket]); got != 1 {
		t.Fatalf("retained queue rows = %d", got)
	}
	retained := engine.buckets[queueBucket][string(queueKey(Incoming, preference.retainedAt))]
	if string(retained) != preference.retained.WireForm() {
		t.Fatalf("retained row = %q", retained)
	}
	got := string(engine.buckets[knownCategoryBucket][categoryA.ID()])
	if got != knownCategoryEvidence(preference.retained) {
		t.Fatalf("rebound evidence = %q", got)
	}
}

func TestQueuedNewsCategoryReconciliationUsesOneBoundedCatalogPass(t *testing.T) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	base := fixedNow().Add(-time.Hour)
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		for index := range maximumKnownNewsRecords {
			record := Record{
				Originator: yagomodel.WordHash(fmt.Sprintf("peer-%04d", index)),
				Created:    base.Add(time.Duration(index%3600) * time.Second),
				Category:   CategoryCrawlStart,
				Attributes: map[string]string{"entry": fmt.Sprintf("%04d", index)},
			}
			key := vault.Key(record.ID())
			if err := pool.known.Put(tx, key, knownMarker); err != nil {
				return fmt.Errorf("store known news marker: %w", err)
			}
			if err := pool.knownCategories.Put(tx, key, knownCategoryEvidence(Record{
				Originator: record.Originator, Created: record.Created,
				Category: CategoryBookmarkAdd, Attributes: record.Attributes,
			})); err != nil {
				return fmt.Errorf("store known news evidence: %w", err)
			}
			if err := pool.queue.Put(
				tx, queueKey(Incoming, uint64(index+1)), record.WireForm(),
			); err != nil {
				return fmt.Errorf("store queued news: %w", err)
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	engine.keyPageReadsByBucket[queueBucket] = 0
	engine.valuePageReads[queueBucket] = 0
	if _, err := pool.buildQueuedNewsEvidenceCatalog(t.Context(), fixedNow()); err != nil {
		t.Fatal(err)
	}
	wantPages := (maximumKnownNewsRecords + newsScrubPage - 1) / newsScrubPage
	if got := engine.keyPageReadsByBucket[queueBucket]; got != wantPages {
		t.Fatalf("queue evidence key-page reads = %d, want %d", got, wantPages)
	}
	if got := engine.valuePageReads[queueBucket]; got != 0 {
		t.Fatalf("queue evidence value-page reads = %d", got)
	}
	if err := pool.pruneQueuedNews(t.Context(), fixedNow()); err != nil {
		t.Fatal(err)
	}
	if len(engine.buckets[queueBucket]) != maximumKnownNewsRecords {
		t.Fatalf("retained queue rows = %d", len(engine.buckets[queueBucket]))
	}
}

func storedKnownCategory(t *testing.T, engine *newsStubEngine, id string) string {
	t.Helper()
	encoded := string(engine.buckets[knownCategoryBucket][id])
	category, _, _, err := decodeKnownCategoryEvidence(encoded)
	if err != nil {
		t.Fatalf("decode stored category for %s: %v", id, err)
	}

	return category
}

func TestKnownNewsCategoryCodecRejectsMalformedEvidence(t *testing.T) {
	codec := knownCategoryCodec{}
	for _, category := range []string{"", "too-long-category"} {
		if _, err := codec.Encode(category); err == nil {
			t.Fatalf("Encode(%q) succeeded", category)
		}
		if _, err := codec.Decode([]byte(category)); err == nil {
			t.Fatalf("Decode(%q) succeeded", category)
		}
	}
	if encoded, err := codec.Encode(CategoryCrawlStart); err != nil ||
		!bytes.Equal(encoded, []byte(CategoryCrawlStart)) {
		t.Fatalf("Encode(valid) = %q/%v", encoded, err)
	}
	if decoded, err := codec.Decode([]byte(CategoryCrawlStart)); err != nil ||
		decoded != CategoryCrawlStart {
		t.Fatalf("Decode(valid) = %q/%v", decoded, err)
	}

	pool := openMemPool(t)
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return pool.replaceKnownNewsCategory(tx, vault.Key("missing"), "")
	}); err != nil {
		t.Fatal(err)
	}
}

func TestKnownNewsLegacyLifetimeRemainsConservativeWithoutCategory(t *testing.T) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	record := retentionRecord(
		fixedNow().Add(-defaultNewsLifetime-time.Hour), 0, CategoryBookmarkAdd,
	)
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return pool.known.Put(tx, vault.Key(record.ID()), knownMarker)
	}); err != nil {
		t.Fatal(err)
	}
	current, expired, err := pool.inspectKnownNews(
		t.Context(), record.ID(), fixedNow(),
	)
	if err != nil || !current || expired {
		t.Fatalf("legacy lifetime = %t/%t/%v", current, expired, err)
	}
	if newsExpired(record.Created, fixedNow(), knownMarker) {
		t.Fatal("legacy marker did not retain the extended lifetime")
	}
	if !newsExpired(record.Created, fixedNow(), record.Category) {
		t.Fatal("exact category did not retain the default lifetime")
	}
}

func TestV0020NewsPoolRejectsInvalidRowsAndStorageFailures(t *testing.T) {
	engine := newNewsStubEngine()
	legacy := openV0020NewsPool(t, engine)
	if stored, err := legacy.enqueueIncomingNews(
		t.Context(), Record{},
	); err != nil || stored {
		t.Fatalf("empty legacy row = %t/%v", stored, err)
	}
	broken := retentionRecord(fixedNow().Add(-time.Hour), 0, CategoryCrawlStart)
	engine.putErrors[knownBucket] = fmt.Errorf("known write failed")
	if _, err := legacy.enqueueIncomingNews(t.Context(), broken); err == nil {
		t.Fatal("v0.0.20 known storage failure was ignored")
	}
}
