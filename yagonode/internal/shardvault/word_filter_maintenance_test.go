package shardvault

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/FastFilter/xorfilter"
	"github.com/cespare/xxhash/v2"
	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestVaultOpenReturnsBeforeWordFilterConstructionAndCloseJoinsIt(t *testing.T) {
	legacyPath := filepath.Join(t.TempDir(), "storage.db")
	seedWordFilterStorage(t, legacyPath+".vault")
	entered, release := blockWordFilterConstruction(t)
	opened := make(chan func() error, 1)
	openFailures := make(chan error, 1)
	go func() {
		storage, err := OpenAt(legacyPath, 1<<20, WithWordFilter(testBucket, testWordWidth))
		if err != nil {
			openFailures <- err

			return
		}
		opened <- storage.Close
	}()
	waitForConstruction(t, entered, openFailures)
	closeStorage := waitForOpen(t, opened, openFailures)
	closed := make(chan error, 1)
	go func() { closed <- closeStorage() }()
	assertStillRunning(t, closed)
	release()
	if err := waitForClose(t, closed); err != nil {
		t.Fatalf("close storage: %v", err)
	}
}

func TestCancelledWordFilterConstructionRetainsConservativeFilter(t *testing.T) {
	e := pendingWordFilterEngine(t)
	putWord(t, e, "word0001", "url")
	index := e.route(testBucket, vault.Key("word0001url"))
	entered, release := blockWordFilterConstruction(t)
	e.startWordFilterMaintenance()
	waitForConstruction(t, entered, nil)
	stopped := make(chan struct{})
	go func() {
		e.stopWordFilterMaintenance()
		close(stopped)
	}()
	assertStillRunning(t, stopped)
	release()
	waitForStop(t, stopped)
	if !e.wordFilters[index].degraded ||
		!e.wordFilters[index].mayContain(xxhash.Sum64String("word0001")) {
		t.Fatal("cancelled construction replaced the conservative filter")
	}
	if err := e.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}
}

func TestPublishedWordFilterCarriesConcurrentSideSet(t *testing.T) {
	e := pendingWordFilterEngine(t)
	putWord(t, e, "word0001", "seed")
	index := e.route(testBucket, vault.Key("word0001seed"))
	word, url := sameShardWordFixture(e, index)
	entered, release := blockWordFilterConstruction(t)
	e.startWordFilterMaintenance()
	waitForConstruction(t, entered, nil)
	putWord(t, e, word, url)
	release()
	waitForWordFilterMaintenance(t, e)
	if e.wordFilters[index].degraded {
		t.Fatal("completed construction remained conservative")
	}
	if !e.wordFilters[index].mayContain(xxhash.Sum64String(word)) {
		t.Fatal("published filter lost a concurrent side-set term")
	}
	if got := scanWord(t, e, word); got != 1 {
		t.Fatalf("concurrent word scan = %d, want 1", got)
	}
	assertOptimizedEmptyShardRejects(t, e, index)
	if err := e.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}
}

func TestWordFilterPagesBoundSnapshotWork(t *testing.T) {
	e := pendingWordFilterEngine(t)
	writeWordFilterPageFixtures(t, e.shards[0], wordFilterMaintenancePageRecords+1)
	first, err := e.readWordFilterPage(t.Context(), 0, nil)
	if err != nil {
		t.Fatalf("read first page: %v", err)
	}
	if len(first.terms) != wordFilterMaintenancePageRecords || first.complete ||
		len(first.last) == 0 {
		t.Fatalf("first page = %d/%t/%x", len(first.terms), first.complete, first.last)
	}
	deleteWordFilterPageFixture(t, e.shards[0], first.last)
	second, err := e.readWordFilterPage(t.Context(), 0, first.last)
	if err != nil {
		t.Fatalf("read second page: %v", err)
	}
	if len(second.terms) != 1 || !second.complete {
		t.Fatalf("second page = %d/%t, want 1/true", len(second.terms), second.complete)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}
}

func TestWordFilterCollectorReadsMultiplePagesAndYieldsToTraffic(t *testing.T) {
	e := pendingWordFilterEngine(t)
	writeWordFilterPageFixtures(t, e.shards[0], wordFilterMaintenancePageRecords+1)
	terms, err := e.collectWordFilterTerms(t.Context(), 0)
	if err != nil || len(terms) != wordFilterMaintenancePageRecords+1 {
		t.Fatalf("idle collection = %d, %v", len(terms), err)
	}
	e.viewsInFlight.Store(1)
	terms, err = e.collectWordFilterTerms(t.Context(), 0)
	if err != nil || len(terms) != wordFilterMaintenancePageRecords+1 {
		t.Fatalf("yielding collection = %d, %v", len(terms), err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}
}

func TestWordFilterCollectorPropagatesYieldFailure(t *testing.T) {
	e := pendingWordFilterEngine(t)
	writeWordFilterPageFixtures(t, e.shards[0], wordFilterMaintenancePageRecords+1)
	e.viewsInFlight.Store(1)
	original := pauseWordFilterMaintenance
	pauseWordFilterMaintenance = func(context.Context) error { return context.Canceled }
	t.Cleanup(func() { pauseWordFilterMaintenance = original })
	if _, err := e.collectWordFilterTerms(t.Context(), 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("collection error = %v, want context cancellation", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}
}

func TestWordFilterPageIgnoresShortKeys(t *testing.T) {
	e := pendingWordFilterEngine(t)
	writeRawWordFilterFixture(t, e.shards[0], []byte("short"))
	page, err := e.readWordFilterPage(t.Context(), 0, nil)
	if err != nil {
		t.Fatalf("read page: %v", err)
	}
	if len(page.terms) != 0 || !page.complete {
		t.Fatalf("short-key page = %d/%t", len(page.terms), page.complete)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}
}

func TestWordFilterPageHonorsCancellation(t *testing.T) {
	e := pendingWordFilterEngine(t)
	writeRawWordFilterFixture(t, e.shards[0], []byte("word0001-url"))
	ctx := &cancellationRaceContext{Context: t.Context(), cancelAt: 3}
	if _, err := e.readWordFilterPage(ctx, 0, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("read error = %v, want context cancellation", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}
}

func TestWordFilterMaintenanceStartIsIdempotent(t *testing.T) {
	e := pendingWordFilterEngine(t)
	e.startWordFilterMaintenance()
	e.startWordFilterMaintenance()
	waitForWordFilterMaintenance(t, e)
	if err := e.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}
}

func TestWordFilterMaintenanceDemandDistinguishesTraffic(t *testing.T) {
	e := &engine{}
	if e.wordFilterMaintenanceDemand() {
		t.Fatal("idle engine reported maintenance demand")
	}
	e.viewsInFlight.Store(1)
	if !e.wordFilterMaintenanceDemand() {
		t.Fatal("interactive read did not request maintenance yield")
	}
	e.viewsInFlight.Store(0)
	e.writeAdmission.concurrent = 1
	if !e.wordFilterMaintenanceDemand() {
		t.Fatal("admitted write did not request maintenance yield")
	}
	e.writeAdmission.concurrent = 0
	e.writeAdmission.pendingContended = 1
	if !e.wordFilterMaintenanceDemand() {
		t.Fatal("pending contended write did not request maintenance yield")
	}
}

func TestWordFilterMaintenanceWaitAcceptsTimeAndRefusesCancellation(t *testing.T) {
	if err := waitWordFilterMaintenance(t.Context()); err != nil {
		t.Fatalf("ordinary wait: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := waitWordFilterMaintenance(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled wait error = %v", err)
	}
}

func TestWordFilterPublicationRefusesCancellationAfterGateAcquisition(t *testing.T) {
	e := pendingWordFilterEngine(t)
	putWord(t, e, "word0001", "url")
	ctx := &cancellationRaceContext{Context: t.Context(), cancelAt: 5}
	index := e.route(testBucket, vault.Key("word0001url"))
	if err := e.optimizeWordFilter(ctx, index); !errors.Is(err, context.Canceled) {
		t.Fatalf("publication error = %v, want context cancellation", err)
	}
	if !e.wordFilters[index].degraded {
		t.Fatal("cancelled publication replaced the conservative filter")
	}
	if err := e.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}
}

func seedWordFilterStorage(t *testing.T, directory string) {
	t.Helper()
	e, err := openEngine(directory, 1<<20)
	if err != nil {
		t.Fatalf("open seed engine: %v", err)
	}
	if err := e.Provision(testBucket); err != nil {
		t.Fatalf("provision seed bucket: %v", err)
	}
	putWord(t, e, "word0001", "url")
	if err := e.Close(); err != nil {
		t.Fatalf("close seed engine: %v", err)
	}
}

func pendingWordFilterEngine(t *testing.T) *engine {
	t.Helper()
	e, err := openEngine(
		filepath.Join(t.TempDir(), "vault"),
		1<<20,
		WithWordFilter(testBucket, testWordWidth),
	)
	if err != nil {
		t.Fatalf("open pending filter engine: %v", err)
	}
	if err := e.Provision(testBucket); err != nil {
		t.Fatalf("provision pending filter engine: %v", err)
	}

	return e
}

func blockWordFilterConstruction(t *testing.T) (<-chan struct{}, func()) {
	t.Helper()
	original := buildFuse
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	buildFuse = func(keys []uint64) (*xorfilter.BinaryFuse[uint8], error) {
		entered <- struct{}{}
		<-release

		return original(keys)
	}
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		buildFuse = original
	})

	return entered, func() { releaseOnce.Do(func() { close(release) }) }
}

func waitForConstruction(t *testing.T, entered <-chan struct{}, failures <-chan error) {
	t.Helper()
	select {
	case <-entered:
	case err := <-failures:
		t.Fatalf("open storage: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("word filter construction did not start")
	}
}

func waitForOpen(t *testing.T, opened <-chan func() error, failures <-chan error) func() error {
	t.Helper()
	select {
	case closeStorage := <-opened:
		return closeStorage
	case err := <-failures:
		t.Fatalf("open storage: %v", err)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("storage open waited for word filter construction")
	}

	return nil
}

func waitForClose(t *testing.T, closed <-chan error) error {
	t.Helper()
	select {
	case err := <-closed:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("storage close did not join word filter maintenance")
	}

	return nil
}

func waitForStop(t *testing.T, stopped <-chan struct{}) {
	t.Helper()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("word filter maintenance did not stop")
	}
}

func assertStillRunning[T any](t *testing.T, completed <-chan T) {
	t.Helper()
	select {
	case <-completed:
		t.Fatal("operation completed before blocked construction was released")
	case <-time.After(20 * time.Millisecond):
	}
}

func sameShardWordFixture(e *engine, index int) (string, string) {
	for candidate := 2; ; candidate++ {
		word := fmt.Sprintf("word%04d", candidate)
		url := fmt.Sprintf("url%d", candidate)
		if e.route(testBucket, vault.Key(word+url)) == index {
			return word, url
		}
	}
}

func assertOptimizedEmptyShardRejects(t *testing.T, e *engine, populated int) {
	t.Helper()
	term := xxhash.Sum64String("word9999")
	for index, filter := range e.wordFilters {
		if index != populated && !filter.degraded && !filter.mayContain(term) {
			return
		}
	}
	t.Fatal("no optimized empty shard rejected an absent term")
}

func writeWordFilterPageFixtures(t *testing.T, database *bolt.DB, total int) {
	t.Helper()
	err := database.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(testBucket))
		for index := range total {
			key := []byte(fmt.Sprintf("%08d-page", index))
			if err := bucket.Put(key, encodeValue([]byte("value"))); err != nil {
				return fmt.Errorf("write fixture record: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("write page fixtures: %v", err)
	}
}

func deleteWordFilterPageFixture(t *testing.T, database *bolt.DB, key []byte) {
	t.Helper()
	err := database.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(testBucket)).Delete(key)
	})
	if err != nil {
		t.Fatalf("delete page fixture: %v", err)
	}
}

func writeRawWordFilterFixture(t *testing.T, database *bolt.DB, key []byte) {
	t.Helper()
	err := database.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(testBucket)).Put(key, encodeValue([]byte("value")))
	})
	if err != nil {
		t.Fatalf("write raw word-filter fixture: %v", err)
	}
}
