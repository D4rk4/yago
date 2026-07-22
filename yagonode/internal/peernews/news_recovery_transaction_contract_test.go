package peernews

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestKnownNewsStoredMarkerRejectsMalformedValue(t *testing.T) {
	if _, err := (knownCodec{}).Decode([]byte("invalid")); !errors.Is(err, ErrBadNewsRecord) {
		t.Fatalf("decode error = %v", err)
	}
}

func TestNewsOpenReportsRecoveryBoundaryFailures(t *testing.T) {
	t.Run("cleanup registration", assertNewsCleanupRegistrationFailure)
	t.Run("rotation recovery", assertNewsOpenRotationRecoveryFailure)
	t.Run("admission recovery", assertNewsOpenAdmissionRecoveryFailure)
	t.Run("cleanup completion", assertNewsOpenCleanupCompletionFailure)
}

func assertNewsCleanupRegistrationFailure(t *testing.T) {
	t.Helper()
	storage := newUnopenedNewsVault(t, newNewsStubEngine())
	if _, err := vault.RegisterKeyspace(storage, cleanupBucket, newsCleanupCodec{}); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(storage, fixedNow); err == nil {
		t.Fatal("Open accepted a duplicate cleanup bucket")
	}
}

func assertNewsOpenRotationRecoveryFailure(t *testing.T) {
	t.Helper()
	failure := errors.New("rotation recovery failed")
	engine := newNewsStubEngine()
	storage := newUnopenedNewsVault(t, engine)
	seedRawNewsCleanup(t, engine, newsRotationKey, "{")
	engine.deleteKeyErrors[cleanupBucket] = map[string]error{string(newsRotationKey): failure}
	if _, err := Open(storage, fixedNow); !errors.Is(err, failure) {
		t.Fatalf("Open error = %v, want %v", err, failure)
	}
}

func assertNewsOpenAdmissionRecoveryFailure(t *testing.T) {
	t.Helper()
	failure := errors.New("admission recovery failed")
	engine := newNewsStubEngine()
	storage := newUnopenedNewsVault(t, engine)
	seedRawNewsCleanup(t, engine, newsAdmissionKey, "{")
	engine.deleteKeyErrors[cleanupBucket] = map[string]error{string(newsAdmissionKey): failure}
	if _, err := Open(storage, fixedNow); !errors.Is(err, failure) {
		t.Fatalf("Open error = %v, want %v", err, failure)
	}
}

func assertNewsOpenCleanupCompletionFailure(t *testing.T) {
	t.Helper()
	failure := errors.New("cleanup completion failed")
	engine := newNewsStubEngine()
	storage := newUnopenedNewsVault(t, engine)
	if err := engine.Provision(knownBucket); err != nil {
		t.Fatal(err)
	}
	record := retentionRecord(fixedNow().Add(-time.Hour), 1, CategoryCrawlStart)
	engine.buckets[knownBucket][record.ID()] = []byte(knownMarker)
	engine.deleteKeyErrors[cleanupBucket] = map[string]error{
		string(knownCleanupCursorKey): failure,
	}
	if _, err := Open(storage, fixedNow); !errors.Is(err, failure) {
		t.Fatalf("Open error = %v, want %v", err, failure)
	}
}

func newUnopenedNewsVault(t *testing.T, engine *newsStubEngine) *vault.Vault {
	t.Helper()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })

	return storage
}

func seedRawNewsCleanup(
	t *testing.T,
	engine *newsStubEngine,
	key vault.Key,
	value string,
) {
	t.Helper()
	if err := engine.Provision(cleanupBucket); err != nil {
		t.Fatal(err)
	}
	engine.buckets[cleanupBucket][string(key)] = []byte(value)
}

func TestNextPublicationReportsPendingRecoveryFailures(t *testing.T) {
	t.Run("rotation", assertNextPublicationRotationRecoveryFailure)
	t.Run("admission", assertNextPublicationAdmissionRecoveryFailure)
}

func assertNextPublicationRotationRecoveryFailure(t *testing.T) {
	t.Helper()
	failure := errors.New("rotation recovery failed")
	pool, engine := openPoolForRecoveryContract(t)
	engine.buckets[cleanupBucket][string(newsRotationKey)] = []byte("{")
	engine.deleteKeyErrors[cleanupBucket] = map[string]error{string(newsRotationKey): failure}
	if _, _, err := pool.NextPublication(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("NextPublication error = %v, want %v", err, failure)
	}
}

func assertNextPublicationAdmissionRecoveryFailure(t *testing.T) {
	t.Helper()
	failure := errors.New("admission recovery failed")
	pool, engine := openPoolForRecoveryContract(t)
	engine.buckets[cleanupBucket][string(newsAdmissionKey)] = []byte("{")
	engine.deleteKeyErrors[cleanupBucket] = map[string]error{string(newsAdmissionKey): failure}
	if _, _, err := pool.NextPublication(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("NextPublication error = %v, want %v", err, failure)
	}
}

func TestStoreNewsRecordReportsPendingRecoveryFailures(t *testing.T) {
	t.Run("rotation", assertStoreNewsRecordRotationRecoveryFailure)
	t.Run("admission", assertStoreNewsRecordAdmissionRecoveryFailure)
}

func assertStoreNewsRecordRotationRecoveryFailure(t *testing.T) {
	t.Helper()
	failure := errors.New("rotation recovery failed")
	pool, engine := openPoolForRecoveryContract(t)
	engine.buckets[cleanupBucket][string(newsRotationKey)] = []byte("{")
	engine.deleteKeyErrors[cleanupBucket] = map[string]error{string(newsRotationKey): failure}
	if _, err := pool.storeNewsRecord(
		t.Context(), recoveryContractRecord(1), fixedNow(), []Queue{Incoming},
	); !errors.Is(err, failure) {
		t.Fatalf("storeNewsRecord error = %v, want %v", err, failure)
	}
}

func assertStoreNewsRecordAdmissionRecoveryFailure(t *testing.T) {
	t.Helper()
	failure := errors.New("admission recovery failed")
	pool, engine := openPoolForRecoveryContract(t)
	engine.buckets[cleanupBucket][string(newsAdmissionKey)] = []byte("{")
	engine.deleteKeyErrors[cleanupBucket] = map[string]error{string(newsAdmissionKey): failure}
	if _, err := pool.storeNewsRecord(
		t.Context(), recoveryContractRecord(2), fixedNow(), []Queue{Incoming},
	); !errors.Is(err, failure) {
		t.Fatalf("storeNewsRecord error = %v, want %v", err, failure)
	}
}

func TestStoreNewsRecordRetainsFailedCompletionIntent(t *testing.T) {
	failure := errors.New("admission completion failed")
	pool, engine := openPoolForRecoveryContract(t)
	engine.deleteKeyErrors[cleanupBucket] = map[string]error{string(newsAdmissionKey): failure}
	record := recoveryContractRecord(3)
	if _, err := pool.storeNewsRecord(
		t.Context(), record, fixedNow(), []Queue{Incoming},
	); !errors.Is(err, failure) {
		t.Fatalf("storeNewsRecord error = %v, want %v", err, failure)
	}
	if !pool.retentionNeedsReconciliation ||
		len(engine.buckets[cleanupBucket][string(newsAdmissionKey)]) == 0 ||
		len(engine.buckets[knownBucket]) != 1 || len(engine.buckets[queueBucket]) != 1 {
		t.Fatalf(
			"failed completion = dirty:%t cleanup:%d known:%d queue:%d",
			pool.retentionNeedsReconciliation,
			len(engine.buckets[cleanupBucket][string(newsAdmissionKey)]),
			len(engine.buckets[knownBucket]),
			len(engine.buckets[queueBucket]),
		)
	}
}

func TestApplyNewsAdmissionHandlesConcurrentKnownRecord(t *testing.T) {
	pool, engine := openPoolForRecoveryContract(t)
	record := recoveryContractRecord(4)
	injected := false
	engine.beforeUpdate = func() {
		if injected {
			return
		}
		injected = true
		engine.buckets[knownBucket][record.ID()] = []byte(knownMarker)
	}
	stored, err := pool.applyNewsAdmission(t.Context(), record, false, []Queue{Incoming})
	if err != nil || stored {
		t.Fatalf("concurrent admission = %t/%v", stored, err)
	}
	queued := len(engine.buckets[queueBucket])
	if queued != 0 || pool.stored != (newsStoredState{}) {
		t.Fatalf("concurrent admission retained queue=%d state=%#v", queued, pool.stored)
	}
}

func TestInspectKnownNewsDistinguishesCategoryFailures(t *testing.T) {
	t.Run("corrupt", assertInspectKnownNewsCorruptCategoryFallback)
	t.Run("operational", assertInspectKnownNewsCategoryStorageFailure)
}

func assertInspectKnownNewsCorruptCategoryFallback(t *testing.T) {
	t.Helper()
	pool, engine := openPoolForRecoveryContract(t)
	record := retentionRecord(
		fixedNow().Add(-defaultNewsLifetime-time.Hour), 5, CategoryBookmarkAdd,
	)
	seedKnownNewsRecord(t, pool, record)
	engine.buckets[knownCategoryBucket][record.ID()] = nil
	current, expired, err := pool.inspectKnownNews(t.Context(), record.ID(), fixedNow())
	if err != nil || !current || expired {
		t.Fatalf("corrupt category fallback = %t/%t/%v", current, expired, err)
	}
}

func assertInspectKnownNewsCategoryStorageFailure(t *testing.T) {
	t.Helper()
	failure := errors.New("category inspection failed")
	pool, engine := openPoolForRecoveryContract(t)
	record := recoveryContractRecord(6)
	seedKnownNewsRecord(t, pool, record)
	engine.valueSizeErrors[knownCategoryBucket] = failure
	current, expired, err := pool.inspectKnownNews(t.Context(), record.ID(), fixedNow())
	if !errors.Is(err, failure) || current || expired {
		t.Fatalf("category storage failure = %t/%t/%v", current, expired, err)
	}
}

func TestNewsIdentityRemovalTraversesAllQueuePages(t *testing.T) {
	pool, engine := openPoolForRecoveryContract(t)
	records := make([]Record, newsScrubPage+1)
	base := fixedNow().Add(-2 * time.Hour)
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		for index := range records {
			records[index] = retentionRecord(base, index, CategoryCrawlStart)
			if err := pool.queue.Put(
				tx, queueKey(Incoming, uint64(index+1)), records[index].WireForm(),
			); err != nil {
				return fmt.Errorf("seed paged news queue: %w", err)
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	engine.keyPageReadsByBucket[queueBucket] = 0
	target := records[len(records)-1]
	if err := pool.removeNewsIdentity(t.Context(), target.ID()); err != nil {
		t.Fatal(err)
	}
	if engine.keyPageReadsByBucket[queueBucket] != 2 {
		t.Fatalf("queue pages = %d, want 2", engine.keyPageReadsByBucket[queueBucket])
	}
	if _, found := engine.buckets[queueBucket][string(queueKey(Incoming, newsScrubPage+1))]; found {
		t.Fatal("target remained after paged identity removal")
	}
}

func TestNewsIdentityRemovalPropagatesQueueInspectionFailure(t *testing.T) {
	failure := errors.New("queue inspection failed")
	pool, engine := openPoolForRecoveryContract(t)
	record := recoveryContractRecord(7)
	key := queueKey(Incoming, 1)
	seedQueuedNewsRecord(t, pool, key, record)
	engine.valueSizeErrors[queueBucket] = failure
	if err := pool.removeNewsIdentity(t.Context(), record.ID()); !errors.Is(err, failure) {
		t.Fatalf("removeNewsIdentity error = %v, want %v", err, failure)
	}
	if _, found := engine.buckets[queueBucket][string(key)]; !found {
		t.Fatal("failed queue inspection removed the row")
	}
}

func TestNewsAdmissionRecoveryReportsPostRemovalReloadFailure(t *testing.T) {
	failure := errors.New("post-removal reload failed")
	pool, engine := openPoolForRecoveryContract(t)
	record := recoveryContractRecord(8)
	storeNewsAdmissionIntent(t, pool, record)
	seedKnownAndQueuedNewsRecord(t, pool, record)
	injected := false
	engine.beforeUpdate = func() {
		if injected || engine.deleteCalls[knownBucket] == 0 {
			return
		}
		injected = true
		engine.keyPageFailureOn = true
		engine.keyPageError = failure
	}
	if err := pool.recoverNewsAdmission(t.Context()); !errors.Is(err, failure) ||
		!pool.retentionNeedsReconciliation {
		t.Fatalf("post-removal recovery = %v dirty=%t", err, pool.retentionNeedsReconciliation)
	}
}

func TestPublicationRotationReportsPreparationFailure(t *testing.T) {
	failure := errors.New("rotation intent failed")
	pool, engine := openPoolForRecoveryContract(t)
	seedOutgoingNewsRecord(t, pool, recoveryContractRecord(9))
	engine.putErrors[cleanupBucket] = failure
	if _, _, err := pool.NextPublication(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("NextPublication error = %v, want %v", err, failure)
	}
}

func TestPublicationRotationReportsAppliedCompletionFailure(t *testing.T) {
	failure := errors.New("rotation completion failed")
	pool, engine := openPoolForRecoveryContract(t)
	seedOutgoingNewsRecord(t, pool, recoveryContractRecord(10))
	engine.deleteKeyErrors[cleanupBucket] = map[string]error{string(newsRotationKey): failure}
	if _, _, err := pool.NextPublication(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("NextPublication error = %v, want %v", err, failure)
	}
	if !pool.retentionNeedsReconciliation {
		t.Fatal("failed rotation completion did not mark retention dirty")
	}
}

func TestPublicationRotationReportsVanishedSourceCompletionFailure(t *testing.T) {
	failure := errors.New("rotation completion failed")
	pool, engine := openPoolForRecoveryContract(t)
	record := recoveryContractRecord(11)
	key := seedOutgoingNewsRecord(t, pool, record)
	removed := false
	engine.beforeUpdate = func() {
		if removed || len(engine.buckets[cleanupBucket][string(newsRotationKey)]) == 0 {
			return
		}
		removed = true
		delete(engine.buckets[queueBucket], string(key))
		engine.deleteKeyErrors[cleanupBucket] = map[string]error{
			string(newsRotationKey): failure,
		}
	}
	if _, _, err := pool.NextPublication(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("NextPublication error = %v, want %v", err, failure)
	}
}

func TestNewsRotationRollbackPropagatesRowInspectionFailure(t *testing.T) {
	failure := errors.New("rotation row inspection failed")
	pool, engine := openPoolForRecoveryContract(t)
	record := recoveryContractRecord(12)
	rotation := newsRotation{
		source: queueKey(Outgoing, 1), original: record,
		rotated: incrementedNewsRecord(record), destination: Outgoing,
	}
	target := queueKey(Outgoing, 2)
	seedQueuedNewsRecord(t, pool, target, rotation.rotated)
	engine.valueSizeErrors[queueBucket] = failure
	if err := pool.rollbackNewsRotation(t.Context(), rotation); !errors.Is(err, failure) {
		t.Fatalf("rollbackNewsRotation error = %v, want %v", err, failure)
	}
	if _, found := engine.buckets[queueBucket][string(target)]; !found {
		t.Fatal("failed rotation inspection removed the target")
	}
	if _, found := engine.buckets[queueBucket][string(rotation.source)]; found {
		t.Fatal("failed rotation inspection restored the source")
	}
}

func openPoolForRecoveryContract(t *testing.T) (*Pool, *newsStubEngine) {
	t.Helper()
	engine := newNewsStubEngine()
	pool := openStubPool(t, engine)
	t.Cleanup(func() { _ = pool.vault.Close() })

	return pool, engine
}

func recoveryContractRecord(index int) Record {
	return retentionRecord(fixedNow().Add(-time.Hour), index, CategoryCrawlStart)
}

func seedKnownNewsRecord(t *testing.T, pool *Pool, record Record) {
	t.Helper()
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return putKnownNewsFixture(pool, tx, record)
	}); err != nil {
		t.Fatal(err)
	}
}

func seedQueuedNewsRecord(t *testing.T, pool *Pool, key vault.Key, record Record) {
	t.Helper()
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := pool.queue.Put(tx, key, record.WireForm()); err != nil {
			return fmt.Errorf("seed queued news record: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func seedKnownAndQueuedNewsRecord(t *testing.T, pool *Pool, record Record) {
	t.Helper()
	if err := pool.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := putKnownNewsFixture(pool, tx, record); err != nil {
			return err
		}
		if err := pool.queue.Put(tx, queueKey(Incoming, 1), record.WireForm()); err != nil {
			return fmt.Errorf("seed queued news record: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func seedOutgoingNewsRecord(t *testing.T, pool *Pool, record Record) vault.Key {
	t.Helper()
	stored, err := pool.storeNewsRecord(
		t.Context(), record, fixedNow(), []Queue{Outgoing},
	)
	if err != nil || !stored {
		t.Fatalf("seed outgoing news = %t/%v", stored, err)
	}

	return queueKey(Outgoing, 1)
}
