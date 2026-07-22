package peernews

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type newsStubEngine struct {
	buckets              map[vault.Name]map[string][]byte
	putErrors            map[vault.Name]error
	deleteErrors         map[vault.Name]error
	deleteKeyErrors      map[vault.Name]map[string]error
	scanErrors           map[vault.Name]error
	beforeScans          map[vault.Name]func()
	valueSizeErrors      map[vault.Name]error
	readErrors           map[vault.Name]error
	missingReads         map[vault.Name]map[string]bool
	valuePageErrors      map[vault.Name]error
	replayUpdate         func(*newsStubEngine)
	beforeUpdate         func()
	beforeView           func()
	pageLimits           []int
	pageAfter            []string
	valuePageReads       map[vault.Name]int
	valueSizeCalls       map[vault.Name]int
	getCalls             map[vault.Name]int
	putCalls             map[vault.Name]int
	deleteCalls          map[vault.Name]int
	failCommit           error
	keyPageLimit         int
	keyPageReads         int
	keyPageReadsByBucket map[vault.Name]int
	keyPageAfterByBucket map[vault.Name][]string
	keyPageError         error
	keyPageFailureOn     bool
	partialCommitTrigger vault.Name
	partialCommitBuckets map[vault.Name]bool
	partialCommitFailure error
	updates              int
}

func newNewsStubEngine() *newsStubEngine {
	return &newsStubEngine{
		buckets:              map[vault.Name]map[string][]byte{},
		putErrors:            map[vault.Name]error{},
		deleteErrors:         map[vault.Name]error{},
		deleteKeyErrors:      map[vault.Name]map[string]error{},
		scanErrors:           map[vault.Name]error{},
		beforeScans:          map[vault.Name]func(){},
		valueSizeErrors:      map[vault.Name]error{},
		readErrors:           map[vault.Name]error{},
		missingReads:         map[vault.Name]map[string]bool{},
		valuePageErrors:      map[vault.Name]error{},
		getCalls:             map[vault.Name]int{},
		putCalls:             map[vault.Name]int{},
		deleteCalls:          map[vault.Name]int{},
		valuePageReads:       map[vault.Name]int{},
		valueSizeCalls:       map[vault.Name]int{},
		keyPageReadsByBucket: map[vault.Name]int{},
		keyPageAfterByBucket: map[vault.Name][]string{},
	}
}

func (e *newsStubEngine) Update(_ context.Context, fn func(vault.EngineTxn) error) error {
	e.updates++
	if e.beforeUpdate != nil {
		e.beforeUpdate()
	}
	commitFailure := e.failCommit
	e.failCommit = nil
	snapshot := cloneNewsBuckets(e.buckets)
	replay := e.replayUpdate
	e.replayUpdate = nil
	if replay == nil {
		err := fn(newsStubTxn{engine: e, writable: true})
		if err != nil {
			e.buckets = snapshot

			return err
		}
		if commitFailure != nil {
			e.buckets = snapshot

			return commitFailure
		}
		if e.partialCommitFailure != nil &&
			newsBucketChanged(snapshot[e.partialCommitTrigger], e.buckets[e.partialCommitTrigger]) {
			failure := e.partialCommitFailure
			e.partialCommitFailure = nil
			committed := cloneNewsBuckets(e.buckets)
			e.buckets = snapshot
			for bucket := range e.partialCommitBuckets {
				e.buckets[bucket] = committed[bucket]
			}

			return failure
		}

		return nil
	}
	if err := fn(newsStubTxn{engine: e, writable: true}); err != nil {
		e.buckets = snapshot

		return err
	}
	replay(e)

	err := fn(newsStubTxn{engine: e, writable: true})
	if err != nil {
		e.buckets = snapshot

		return err
	}
	if commitFailure != nil {
		e.buckets = snapshot

		return commitFailure
	}

	return nil
}

func newsBucketChanged(before, after map[string][]byte) bool {
	if len(before) != len(after) {
		return true
	}
	for key, beforeValue := range before {
		if !bytes.Equal(beforeValue, after[key]) {
			return true
		}
	}

	return false
}

func cloneNewsBuckets(source map[vault.Name]map[string][]byte) map[vault.Name]map[string][]byte {
	cloned := make(map[vault.Name]map[string][]byte, len(source))
	for name, bucket := range source {
		cloned[name] = make(map[string][]byte, len(bucket))
		for key, value := range bucket {
			cloned[name][key] = append([]byte(nil), value...)
		}
	}

	return cloned
}

func (e *newsStubEngine) View(_ context.Context, fn func(vault.EngineTxn) error) error {
	if e.beforeView != nil {
		e.beforeView()
	}
	return fn(newsStubTxn{engine: e})
}

func (e *newsStubEngine) Provision(name vault.Name) error {
	if e.buckets[name] == nil {
		e.buckets[name] = map[string][]byte{}
	}

	return nil
}

func (e *newsStubEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }
func (e *newsStubEngine) QuotaBytes() int64                        { return 0 }
func (e *newsStubEngine) Close() error                             { return nil }

type newsStubTxn struct {
	engine   *newsStubEngine
	writable bool
}

func (t newsStubTxn) Bucket(name vault.Name) vault.EngineBucket {
	return newsStubBucket{engine: t.engine, name: name}
}

func (t newsStubTxn) Writable() bool { return t.writable }

type newsStubBucket struct {
	engine *newsStubEngine
	name   vault.Name
}

func (b newsStubBucket) Get(key vault.Key) []byte {
	b.engine.getCalls[b.name]++
	raw := b.engine.buckets[b.name][string(key)]
	if raw == nil {
		return nil
	}

	return append([]byte(nil), raw...)
}

func (b newsStubBucket) ReadValue(key vault.Key) ([]byte, bool, error) {
	b.engine.getCalls[b.name]++
	raw, found := b.engine.buckets[b.name][string(key)]
	if err := b.engine.readErrors[b.name]; err != nil {
		return nil, found, err
	}
	if b.engine.missingReads[b.name][string(key)] {
		return nil, false, nil
	}
	if !found {
		return nil, false, nil
	}

	return append([]byte(nil), raw...), true, nil
}

func (b newsStubBucket) Contains(key vault.Key) bool {
	_, found := b.engine.buckets[b.name][string(key)]

	return found
}

func (b newsStubBucket) Put(key vault.Key, raw []byte) error {
	b.engine.putCalls[b.name]++
	if err := b.engine.putErrors[b.name]; err != nil {
		return err
	}
	b.engine.buckets[b.name][string(key)] = append([]byte(nil), raw...)

	return nil
}

func (b newsStubBucket) Delete(key vault.Key) error {
	b.engine.deleteCalls[b.name]++
	if err := b.engine.deleteKeyErrors[b.name][string(key)]; err != nil {
		return err
	}
	if err := b.engine.deleteErrors[b.name]; err != nil {
		return err
	}
	delete(b.engine.buckets[b.name], string(key))

	return nil
}

func (b newsStubBucket) Scan(prefix vault.Key, fn func(vault.Key, []byte) (bool, error)) error {
	if before := b.engine.beforeScans[b.name]; before != nil {
		before()
	}
	if err := b.engine.scanErrors[b.name]; err != nil {
		return err
	}
	keys := make([]string, 0, len(b.engine.buckets[b.name]))
	for key := range b.engine.buckets[b.name] {
		if bytes.HasPrefix([]byte(key), prefix) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	for _, key := range keys {
		again, err := fn(vault.Key(key), append([]byte(nil), b.engine.buckets[b.name][key]...))
		if err != nil || !again {
			return err
		}
	}

	return nil
}

func (b newsStubBucket) ReadPageAfter(after vault.Key, limit int) (vault.BucketPage, error) {
	b.engine.valuePageReads[b.name]++
	if err := b.engine.valuePageErrors[b.name]; err != nil {
		return vault.BucketPage{}, err
	}
	b.engine.pageLimits = append(b.engine.pageLimits, limit)
	b.engine.pageAfter = append(b.engine.pageAfter, string(after))
	keys := make([]string, 0, len(b.engine.buckets[b.name]))
	for key := range b.engine.buckets[b.name] {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	start := 0
	if after != nil {
		var found bool
		start, found = slices.BinarySearch(keys, string(after))
		if found {
			start++
		}
	}
	end := min(start+limit, len(keys))
	entries := make([]vault.BucketPageEntry, 0, end-start)
	for _, key := range keys[start:end] {
		entries = append(entries, vault.BucketPageEntry{
			Key: vault.Key(key), Value: append([]byte(nil), b.engine.buckets[b.name][key]...),
		})
	}

	return vault.BucketPage{Entries: entries, More: end < len(keys)}, nil
}

func (b newsStubBucket) ReadKeyPageAfter(after vault.Key, limit int) (vault.BucketKeyPage, error) {
	if b.engine.keyPageFailureOn ||
		(b.engine.keyPageLimit > 0 && b.engine.keyPageReads >= b.engine.keyPageLimit) {
		return vault.BucketKeyPage{}, b.engine.keyPageError
	}
	b.engine.keyPageReads++
	b.engine.keyPageReadsByBucket[b.name]++
	b.engine.keyPageAfterByBucket[b.name] = append(
		b.engine.keyPageAfterByBucket[b.name],
		string(after),
	)
	b.engine.pageLimits = append(b.engine.pageLimits, limit)
	b.engine.pageAfter = append(b.engine.pageAfter, string(after))
	keys := make([]string, 0, len(b.engine.buckets[b.name]))
	for key := range b.engine.buckets[b.name] {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	start := 0
	if after != nil {
		var found bool
		start, found = slices.BinarySearch(keys, string(after))
		if found {
			start++
		}
	}
	end := min(start+limit, len(keys))
	page := make([]vault.Key, 0, end-start)
	for _, key := range keys[start:end] {
		page = append(page, vault.Key(key))
	}

	return vault.BucketKeyPage{Keys: page, More: end < len(keys)}, nil
}

func (b newsStubBucket) ValueSize(key vault.Key) (int, bool, error) {
	b.engine.valueSizeCalls[b.name]++
	if err := b.engine.valueSizeErrors[b.name]; err != nil {
		return 0, false, err
	}
	value, found := b.engine.buckets[b.name][string(key)]

	return len(value), found, nil
}

func openStubPool(t *testing.T, engine *newsStubEngine) *Pool {
	return openStubPoolAt(t, engine, fixedNow)
}

func openStubPoolAt(
	t *testing.T,
	engine *newsStubEngine,
	now func() time.Time,
) *Pool {
	t.Helper()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	pool, err := Open(storage, now)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	return pool
}

func TestOpenRejectsDuplicateBuckets(t *testing.T) {
	for _, bucket := range []vault.Name{
		queueBucket, knownBucket, knownCategoryBucket, cursorBucket,
	} {
		storage, err := vault.New(newNewsStubEngine())
		if err != nil {
			t.Fatalf("vault.New: %v", err)
		}
		if _, err := vault.Register(storage, bucket, wireCodec{}); err != nil {
			t.Fatalf("pre-register %s: %v", bucket, err)
		}
		if _, err := Open(storage, fixedNow); err == nil {
			t.Errorf("Open with taken bucket %s did not fail", bucket)
		}
	}
}

func TestPublishOwnNewsReturnsStorageErrors(t *testing.T) {
	ctx := context.Background()
	originator := yagomodel.WordHash("myseed")

	knownBroken := newNewsStubEngine()
	knownBroken.putErrors[knownBucket] = errors.New("known put failed")
	if err := openStubPool(
		t,
		knownBroken,
	).PublishOwnNews(ctx, originator, "TestCat", nil); err == nil {
		t.Error("known put failure did not fail")
	}

	categoryBroken := newNewsStubEngine()
	categoryBroken.putErrors[knownCategoryBucket] = errors.New("category put failed")
	if err := openStubPool(
		t,
		categoryBroken,
	).PublishOwnNews(ctx, originator, "TestCat", nil); err == nil {
		t.Error("category put failure did not fail")
	}
	if len(categoryBroken.buckets[knownBucket]) != 0 ||
		len(categoryBroken.buckets[knownCategoryBucket]) != 0 ||
		len(categoryBroken.buckets[queueBucket]) != 0 {
		t.Fatal("category failure left a partial callback transaction")
	}

	queueBroken := newNewsStubEngine()
	queueBroken.putErrors[queueBucket] = errors.New("queue put failed")
	if err := openStubPool(
		t,
		queueBroken,
	).PublishOwnNews(ctx, originator, "TestCat", nil); err == nil {
		t.Error("queue put failure did not fail")
	}

	cursorBroken := newNewsStubEngine()
	cursorBroken.putErrors[cursorBucket] = errors.New("cursor put failed")
	if err := openStubPool(
		t,
		cursorBroken,
	).PublishOwnNews(ctx, originator, "TestCat", nil); err == nil {
		t.Error("cursor put failure did not fail")
	}

	knownCorrupt := newNewsStubEngine()
	pool := openStubPool(t, knownCorrupt)
	if err := pool.PublishOwnNews(ctx, originator, "TestCat", nil); err != nil {
		t.Fatalf("PublishOwnNews: %v", err)
	}
	for key := range knownCorrupt.buckets[knownBucket] {
		knownCorrupt.buckets[knownBucket][key] = []byte("corrupt")
	}
	if err := pool.PublishOwnNews(ctx, originator, "TestCat", nil); err == nil {
		t.Error("corrupt known marker did not fail")
	}

	cursorCorrupt := newNewsStubEngine()
	pool = openStubPool(t, cursorCorrupt)
	cursorCorrupt.buckets[cursorBucket][string(Incoming)] = []byte("not-a-number")
	if err := pool.PublishOwnNews(ctx, originator, "TestCat", nil); err == nil {
		t.Error("corrupt cursor did not fail")
	}
}

func TestNextPublicationReturnsStorageErrors(t *testing.T) {
	ctx := context.Background()
	originator := yagomodel.WordHash("myseed")

	scanBroken := newNewsStubEngine()
	pool := openStubPool(t, scanBroken)
	if err := pool.PublishOwnNews(ctx, originator, "TestCat", nil); err != nil {
		t.Fatalf("PublishOwnNews: %v", err)
	}
	scanBroken.scanErrors[queueBucket] = errors.New("scan failed")
	if _, _, err := pool.NextPublication(ctx); err == nil {
		t.Error("scan failure did not fail")
	}

	deleteBroken := newNewsStubEngine()
	pool = openStubPool(t, deleteBroken)
	if err := pool.PublishOwnNews(ctx, originator, "TestCat", nil); err != nil {
		t.Fatalf("PublishOwnNews: %v", err)
	}
	deleteBroken.deleteErrors[queueBucket] = errors.New("delete failed")
	if _, _, err := pool.NextPublication(ctx); err == nil {
		t.Error("delete failure did not fail")
	}

	corrupt := newNewsStubEngine()
	pool = openStubPool(t, corrupt)
	corrupt.buckets[queueBucket][string(queueKey(Outgoing, 1))] = []byte("{dis=many}")
	if _, found, err := pool.NextPublication(ctx); err != nil || found {
		t.Errorf("corrupt stored record = %t/%v, want discarded", found, err)
	}
}

func TestEnqueueIncomingNewsReturnsStorageErrors(t *testing.T) {
	ctx := context.Background()
	record := Record{
		Originator: yagomodel.WordHash("peer"),
		Created:    fixedNow(),
		Category:   CategoryCrawlStart,
	}

	broken := newNewsStubEngine()
	broken.putErrors[knownBucket] = errors.New("known put failed")
	if _, err := openStubPool(t, broken).EnqueueIncomingNews(ctx, record); err == nil {
		t.Error("known put failure did not fail")
	}

	corrupt := newNewsStubEngine()
	pool := openStubPool(t, corrupt)
	if _, err := pool.EnqueueIncomingNews(ctx, record); err != nil {
		t.Fatalf("EnqueueIncomingNews: %v", err)
	}
	for key := range corrupt.buckets[knownBucket] {
		corrupt.buckets[knownBucket][key] = []byte("corrupt")
	}
	if _, err := pool.EnqueueIncomingNews(ctx, record); err == nil {
		t.Error("corrupt known marker did not fail")
	}
}

func TestByIDReturnsStorageErrors(t *testing.T) {
	ctx := context.Background()

	scanBroken := newNewsStubEngine()
	pool := openStubPool(t, scanBroken)
	scanBroken.scanErrors[queueBucket] = errors.New("scan failed")
	if _, _, err := pool.ByID(ctx, Published, "x"); err == nil {
		t.Error("scan failure did not fail")
	}

	corrupt := newNewsStubEngine()
	pool = openStubPool(t, corrupt)
	corrupt.buckets[queueBucket][string(queueKey(Published, 1))] = []byte("{dis=many}")
	if _, _, err := pool.ByID(ctx, Published, "x"); err == nil {
		t.Error("corrupt stored record did not fail")
	}
}

func TestRecentReturnsStorageErrors(t *testing.T) {
	ctx := context.Background()

	scanBroken := newNewsStubEngine()
	pool := openStubPool(t, scanBroken)
	scanBroken.scanErrors[queueBucket] = errors.New("scan failed")
	if _, err := pool.Recent(ctx, Incoming, 5); err == nil {
		t.Error("scan failure did not fail")
	}

	corrupt := newNewsStubEngine()
	pool = openStubPool(t, corrupt)
	corrupt.buckets[queueBucket][string(queueKey(Incoming, 1))] = []byte("{dis=many}")
	if _, err := pool.Recent(ctx, Incoming, 5); err == nil {
		t.Error("corrupt stored record did not fail")
	}
}

func TestNewsPoolSurvivesReopenOnSameStorage(t *testing.T) {
	ctx := context.Background()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	if err := pool.PublishOwnNews(ctx, yagomodel.WordHash("myseed"), "TestCat", nil); err != nil {
		t.Fatalf("PublishOwnNews: %v", err)
	}

	reopened := openStubPool(t, engine)
	record, ok, err := reopened.NextPublication(ctx)
	if err != nil || !ok {
		t.Fatalf("NextPublication after reopen = %v, %v", ok, err)
	}
	if record.Distributed != 1 {
		t.Fatalf("distributed = %d, want 1", record.Distributed)
	}
}

func TestNextPublicationClearsAbortedAttemptResult(t *testing.T) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	if err := pool.PublishOwnNews(
		t.Context(),
		yagomodel.WordHash("myseed"),
		"TestCat",
		nil,
	); err != nil {
		t.Fatal(err)
	}
	engine.replayUpdate = func(engine *newsStubEngine) {
		engine.buckets[queueBucket] = map[string][]byte{}
	}
	record, found, err := pool.NextPublication(t.Context())
	if err != nil || found {
		t.Fatalf("publication = %#v/%v/%v", record, found, err)
	}
}

func TestEnqueueIncomingNewsClearsAbortedAttemptResult(t *testing.T) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	record := Record{
		Originator: yagomodel.WordHash("peer"),
		Created:    fixedNow(),
		Category:   CategoryCrawlStart,
	}
	admission, retained := pool.prepareNewsRecordAdmission(record, false, []Queue{Incoming})
	if !retained {
		t.Fatal("record was not admitted")
	}
	engine.replayUpdate = func(*newsStubEngine) {}
	stored, err := pool.persistNewsRecord(t.Context(), record, false, &admission)
	if err != nil || stored {
		t.Fatalf("stored = %v/%v, want false after replay", stored, err)
	}
	admission.rollback()
}

func TestNewsRetentionStateRollsBackAfterCommitFailure(t *testing.T) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	pool.retention = newsRetention{
		queueRecords: 2, queueBytes: maximumNewsQueueBytes, knownRecords: 2,
	}
	first := retentionRecord(fixedNow().Add(-time.Hour), 1, CategoryCrawlStart)
	second := retentionRecord(fixedNow().Add(-time.Hour), 2, CategoryCrawlStart)
	if stored, err := pool.EnqueueIncomingNews(t.Context(), first); err != nil || !stored {
		t.Fatalf("first enqueue = %t/%v", stored, err)
	}
	before := pool.stored
	sentinel := errors.New("commit failed")
	engine.failCommit = sentinel
	if _, err := pool.EnqueueIncomingNews(t.Context(), second); !errors.Is(err, sentinel) {
		t.Fatalf("second enqueue error = %v, want %v", err, sentinel)
	}
	if pool.stored != before ||
		!pool.knownNewsRetention.Contains(vault.Key(first.ID())) ||
		pool.knownNewsRetention.Contains(vault.Key(second.ID())) {
		t.Fatalf("retention state after failed commit = %#v", pool.stored)
	}
	if _, found, err := pool.ByID(t.Context(), Incoming, first.ID()); err != nil || !found {
		t.Fatalf("first record after failed commit = %t/%v", found, err)
	}
	if _, found, err := pool.ByID(t.Context(), Incoming, second.ID()); err != nil || found {
		t.Fatalf("second record after failed commit = %t/%v", found, err)
	}
}

func TestNewsAdmissionRecoversPartialBucketsAcrossReopen(t *testing.T) {
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	record := retentionRecord(fixedNow().Add(-time.Hour), 7, CategoryCrawlStart)
	failure := errors.New("later shard commit failed")
	engine.partialCommitTrigger = knownBucket
	engine.partialCommitBuckets = map[vault.Name]bool{
		knownBucket: true, knownCategoryBucket: true,
	}
	engine.partialCommitFailure = failure
	if stored, err := pool.EnqueueIncomingNews(
		t.Context(),
		record,
	); !errors.Is(err, failure) ||
		stored {
		t.Fatalf("partial enqueue = %t/%v, want false/%v", stored, err, failure)
	}
	if len(engine.buckets[knownBucket]) != 1 || len(engine.buckets[queueBucket]) != 0 ||
		len(engine.buckets[cleanupBucket][string(newsAdmissionKey)]) == 0 {
		t.Fatalf(
			"partial state = known %d queue %d intent %q",
			len(engine.buckets[knownBucket]),
			len(engine.buckets[queueBucket]),
			engine.buckets[cleanupBucket][string(newsAdmissionKey)],
		)
	}
	if err := pool.vault.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openStubPool(t, engine)
	recovered, found, err := reopened.ByID(t.Context(), Incoming, record.ID())
	if err != nil || !found || recovered.ID() != record.ID() {
		t.Fatalf("recovered record = %#v/%t/%v", recovered, found, err)
	}
	if reopened.stored.knownRecords != 1 || reopened.stored.queueRecords != 1 ||
		len(engine.buckets[knownBucket]) != 1 || len(engine.buckets[queueBucket]) != 1 ||
		len(engine.buckets[cleanupBucket]) != 0 {
		t.Fatalf(
			"reloaded retention = %#v known=%d queue=%d cleanup=%d",
			reopened.stored,
			len(engine.buckets[knownBucket]),
			len(engine.buckets[queueBucket]),
			len(engine.buckets[cleanupBucket]),
		)
	}
	if duplicate, err := reopened.EnqueueIncomingNews(
		t.Context(),
		record,
	); err != nil ||
		duplicate {
		t.Fatalf("recovered duplicate = %t/%v", duplicate, err)
	}
	if len(engine.buckets[queueBucket]) != 1 {
		t.Fatalf("queue rows after duplicate = %d, want 1", len(engine.buckets[queueBucket]))
	}
}
