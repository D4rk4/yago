package crawlbroker

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestLegacyLeaseSettlementExpiresAtFixedHorizon(t *testing.T) {
	set := withClock(t)
	base := time.Unix(50_000, 0)
	set(base)
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "retention-edge", "worker")
	if _, err := queue.ackLeaseWithTarget(t.Context(), leaseID); err != nil {
		t.Fatalf("ack lease: %v", err)
	}
	if err := queue.completeRunControl(t.Context(), leaseID); err != nil {
		t.Fatalf("complete control: %v", err)
	}

	set(base.Add(leaseSettlementRetention - time.Nanosecond))
	if err := queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("sweep before horizon: %v", err)
	}
	if err := queue.ackLease(t.Context(), leaseID); err != nil {
		t.Fatalf("retry before horizon: %v", err)
	}
	assertLeaseSettlementRetentionCounts(t, queue, leaseSettlementRows{1, 1, 1, 1, 0})

	set(base.Add(leaseSettlementRetention))
	if err := queue.ackLease(t.Context(), leaseID); !errors.Is(err, errLeaseDispositionConflict) {
		t.Fatalf("retry at horizon = %v", err)
	}
	assertLeaseSettlementRetentionCounts(t, queue, leaseSettlementRows{1, 1, 1, 1, 0})
	if err := queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("sweep at horizon: %v", err)
	}
	if err := queue.deferLease(t.Context(), leaseID); !errors.Is(err, errLeaseDispositionConflict) {
		t.Fatalf("nak at horizon = %v", err)
	}
	assertLeaseSettlementRetentionCounts(t, queue, leaseSettlementRows{})
}

func TestExpiredSettlementPreservesPendingControlUntilReplay(t *testing.T) {
	set := withClock(t)
	base := time.Unix(60_000, 0)
	set(base)
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "pending-control", "worker")
	if _, err := queue.ackLeaseWithTarget(t.Context(), leaseID); err != nil {
		t.Fatalf("ack lease: %v", err)
	}

	set(base.Add(leaseSettlementRetention))
	if err := queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("expire settlement: %v", err)
	}
	assertLeaseSettlementRetentionCounts(t, queue, leaseSettlementRows{pendingControls: 1})
	if err := queue.ackLease(t.Context(), leaseID); !errors.Is(err, errLeaseDispositionConflict) {
		t.Fatalf("late ack with pending control = %v", err)
	}
	if err := queue.replayRunControlCompletions(t.Context(), newControlRegistry()); err != nil {
		t.Fatalf("replay pending control: %v", err)
	}
	assertLeaseSettlementRetentionCounts(t, queue, leaseSettlementRows{})
}

func TestLeaseSettlementRetentionSurvivesRestart(t *testing.T) {
	set := withClock(t)
	base := time.Unix(70_000, 0)
	set(base)
	path := filepath.Join(t.TempDir(), "node.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open first storage: %v", err)
	}
	first, err := newDurableOrderQueue(storage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("open first queue: %v", err)
	}
	leaseID := leaseOne(t, first, "restart-retention", "worker")
	if _, err := first.ackLeaseWithTarget(t.Context(), leaseID); err != nil {
		t.Fatalf("ack lease: %v", err)
	}
	if err := first.completeRunControl(t.Context(), leaseID); err != nil {
		t.Fatalf("complete control: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close first storage: %v", err)
	}

	set(base.Add(leaseSettlementRetention))
	storage, err = boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open second storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	second, err := newDurableOrderQueue(storage, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("open second queue: %v", err)
	}
	if err := second.ackLease(t.Context(), leaseID); !errors.Is(err, errLeaseDispositionConflict) {
		t.Fatalf("retry after restart horizon = %v", err)
	}
	assertLeaseSettlementRetentionCounts(t, second, leaseSettlementRows{1, 1, 1, 1, 0})
	if err := second.sweepExpired(t.Context()); err != nil {
		t.Fatalf("expire after restart: %v", err)
	}
	assertLeaseSettlementRetentionCounts(t, second, leaseSettlementRows{})
}

func TestRequeuedLeaseSettlementsUseFixedRetention(t *testing.T) {
	set := withClock(t)
	for _, test := range []struct {
		name   string
		settle func(*testing.T, *DurableOrderQueue, string, time.Time, func(time.Time)) time.Time
	}{
		{
			name: "negative acknowledgment",
			settle: func(
				t *testing.T,
				queue *DurableOrderQueue,
				leaseID string,
				base time.Time,
				set func(time.Time),
			) time.Time {
				if err := queue.deferLease(t.Context(), leaseID); err != nil {
					t.Fatalf("defer lease: %v", err)
				}
				set(base.Add(negativeAcknowledgmentRetryDelay))
				if err := queue.sweepExpired(t.Context()); err != nil {
					t.Fatalf("requeue deferred lease: %v", err)
				}

				return base
			},
		},
		{
			name: "expired lease",
			settle: func(
				t *testing.T,
				queue *DurableOrderQueue,
				_ string,
				base time.Time,
				set func(time.Time),
			) time.Time {
				settledAt := base.Add(DefaultLeaseTTL)
				set(settledAt)
				if err := queue.sweepExpired(t.Context()); err != nil {
					t.Fatalf("expire lease: %v", err)
				}

				return settledAt
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			base := time.Unix(75_000, 0)
			set(base)
			queue := memQueue(t)
			leaseID := leaseOne(t, queue, test.name, "worker")
			settledAt := test.settle(t, queue, leaseID, base, set)
			set(settledAt.Add(leaseSettlementRetention))
			if err := queue.sweepExpired(t.Context()); err != nil {
				t.Fatalf("expire requeued settlement: %v", err)
			}
			assertLeaseSettlementRetentionCounts(t, queue, leaseSettlementRows{})
		})
	}
}

func TestLegacyLeaseSettlementRetentionRemainsBounded(t *testing.T) {
	set := withClock(t)
	base := time.Unix(80_000, 0)
	set(base)
	queue := memQueue(t)
	const settlements = 10_000
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		for index := 0; index < settlements; index++ {
			if err := queue.recordLeaseSettlement(
				tx,
				fmt.Sprintf("legacy-%05d", index),
				leaseSettlementAcknowledged,
			); err != nil {
				return err
			}
		}
		_, err := queue.recordTerminalLeaseSettlement(tx, "rich", leaseSettlementRecord{
			Outcome:         leaseSettlementAcknowledged,
			OrderIdentity:   make([]byte, 32),
			WorkerSessionID: "session",
			Progress: yagocrawlcontract.CrawlRunProgress{
				WorkerID: "worker",
				RunID:    "run",
				State:    yagocrawlcontract.CrawlRunFinished,
			},
			Terminal: true,
		})

		return err
	}); err != nil {
		t.Fatalf("record settlements: %v", err)
	}
	set(base.Add(leaseSettlementRetention))
	if err := queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("first bounded sweep: %v", err)
	}
	assertLeaseSettlementRetentionCounts(t, queue, leaseSettlementRows{
		settlements:   settlements - maximumLeaseSettlementRetentionChunk + 1,
		orderEntries:  settlements - maximumLeaseSettlementRetentionChunk + 1,
		expiryEntries: settlements - maximumLeaseSettlementRetentionChunk,
	})
	for pass := 1; pass < 40; pass++ {
		if err := queue.sweepExpired(t.Context()); err != nil {
			t.Fatalf("bounded sweep %d: %v", pass, err)
		}
	}
	assertLeaseSettlementRetentionCounts(
		t,
		queue,
		leaseSettlementRows{settlements: 1, orderEntries: 1},
	)
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		record, found, err := queue.leaseSettlements.Get(tx, vault.Key("rich"))
		if err != nil || !found || !record.Terminal {
			t.Fatalf("rich settlement = %+v found=%v err=%v", record, found, err)
		}

		return nil
	}); err != nil {
		t.Fatalf("read rich settlement: %v", err)
	}
}

func TestLegacySettlementWithoutTimestampMigratesBeforeExpiry(t *testing.T) {
	set := withClock(t)
	base := time.Unix(90_000, 0)
	set(base)
	queue := memQueue(t)
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := queue.leaseSettlements.Put(tx, vault.Key("legacy"), leaseSettlementRecord{
			Outcome:  leaseSettlementRequeued,
			Sequence: 0,
		}); err != nil {
			return fmt.Errorf("store legacy settlement: %w", err)
		}
		if err := queue.leaseSettlementOrder.Put(tx, orderKey(0), []byte("legacy")); err != nil {
			return fmt.Errorf("store legacy settlement order: %w", err)
		}

		if err := queue.seq.Put(tx, leaseSettlementNextKey, 1); err != nil {
			return fmt.Errorf("store legacy settlement sequence: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("record old settlement: %v", err)
	}
	if err := queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("migrate old settlement: %v", err)
	}
	assertLeaseSettlementRetentionCounts(t, queue, leaseSettlementRows{1, 1, 1, 0, 0})
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		record, found, err := queue.leaseSettlements.Get(tx, vault.Key("legacy"))
		if err != nil || !found || record.SettledAtUnixNano != base.UnixNano() {
			t.Fatalf("migrated settlement = %+v found=%v err=%v", record, found, err)
		}

		return nil
	}); err != nil {
		t.Fatalf("read migrated settlement: %v", err)
	}
	set(base.Add(leaseSettlementRetention))
	if err := queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("expire migrated settlement: %v", err)
	}
	assertLeaseSettlementRetentionCounts(t, queue, leaseSettlementRows{})
}

func TestLegacySettlementWithoutTimestampStartsHorizonOnRetry(t *testing.T) {
	set := withClock(t)
	base := time.Unix(95_000, 0)
	set(base)
	queue := memQueue(t)
	putUnmigratedLeaseSettlement(t, queue, "legacy")
	if err := queue.ackLease(t.Context(), "legacy"); err != nil {
		t.Fatalf("retry old settlement: %v", err)
	}
	assertLeaseSettlementRetentionCounts(t, queue, leaseSettlementRows{1, 1, 1, 0, 0})
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		record, found, err := queue.leaseSettlements.Get(tx, vault.Key("legacy"))
		if err != nil || !found || record.SettledAtUnixNano != base.UnixNano() {
			t.Fatalf("retried settlement = %+v found=%v err=%v", record, found, err)
		}

		return nil
	}); err != nil {
		t.Fatalf("read retried settlement: %v", err)
	}
	set(base.Add(leaseSettlementRetention))
	if err := queue.ackLease(t.Context(), "legacy"); !errors.Is(err, errLeaseDispositionConflict) {
		t.Fatalf("retry old settlement at horizon = %v", err)
	}
}

func TestLeaseSettlementExpiryIdentityRejectsInvalidKeys(t *testing.T) {
	if _, err := leaseSettlementExpiryIdentity(vault.Key{1}); err == nil {
		t.Fatal("short expiry identity was accepted")
	}
	if _, err := leaseSettlementExpiryIdentity(make(vault.Key, 16)); err == nil {
		t.Fatal("zero expiry time was accepted")
	}
}

func TestIdleLeaseSettlementRetentionDoesNotWrite(t *testing.T) {
	fixture := scriptedQueue(t)
	fixture.engine.replayNext = true
	if err := fixture.queue.expireLeaseSettlements(t.Context(), time.Now()); err != nil {
		t.Fatalf("idle retention: %v", err)
	}
	if !fixture.engine.replayNext {
		t.Fatal("idle retention opened a write transaction")
	}
}

type leaseSettlementMigrationFailure struct {
	name    string
	prepare func(*testing.T, *scriptedQueueFixture)
}

func leaseSettlementMigrationFailures() []leaseSettlementMigrationFailure {
	return []leaseSettlementMigrationFailure{
		{"next sequence", prepareInvalidSettlementSequence},
		{"migration cursor", prepareInvalidSettlementMigrationCursor},
		{"order index", prepareInvalidSettlementOrderIndex},
		{"settlement record", prepareInvalidSettlementRecord},
		{"orphan index delete", prepareSettlementOrphanDeleteFailure},
		{"sequence mismatch", prepareSettlementSequenceMismatch},
		{"settlement time", prepareSettlementTimeFailure},
		{"expiry index", prepareSettlementExpiryIndexFailure},
		{"advance cursor", prepareSettlementCursorAdvanceFailure},
	}
}

func TestLeaseSettlementMigrationSurfacesStorageFailures(t *testing.T) {
	set := withClock(t)
	base := time.Unix(100_000, 0)
	set(base)
	for _, test := range leaseSettlementMigrationFailures() {
		t.Run(test.name, func(t *testing.T) {
			fixture := scriptedQueue(t)
			test.prepare(t, &fixture)
			if err := fixture.queue.expireLeaseSettlements(t.Context(), base); err == nil {
				t.Fatal("migration storage failure was hidden")
			}
		})
	}
}

func prepareInvalidSettlementSequence(_ *testing.T, fixture *scriptedQueueFixture) {
	fixture.engine.buckets[seqBucket][string(leaseSettlementNextKey)] = []byte{1}
}

func prepareInvalidSettlementMigrationCursor(t *testing.T, fixture *scriptedQueueFixture) {
	putUnmigratedLeaseSettlement(t, fixture.queue, "lease")
	fixture.engine.buckets[seqBucket][string(leaseSettlementMigrationNextKey)] = []byte{1}
}

func prepareInvalidSettlementOrderIndex(t *testing.T, fixture *scriptedQueueFixture) {
	putUnmigratedLeaseSettlement(t, fixture.queue, "lease")
	fixture.engine.buckets[leaseSettlementOrderBucket][string(orderKey(0))] = []byte{0}
}

func prepareInvalidSettlementRecord(t *testing.T, fixture *scriptedQueueFixture) {
	putUnmigratedLeaseSettlement(t, fixture.queue, "lease")
	fixture.engine.buckets[leaseSettlementBucket]["lease"] = []byte{1}
}

func prepareSettlementOrphanDeleteFailure(t *testing.T, fixture *scriptedQueueFixture) {
	putUnmigratedLeaseSettlement(t, fixture.queue, "lease")
	delete(fixture.engine.buckets[leaseSettlementBucket], "lease")
	fixture.engine.deleteErrors[leaseSettlementOrderBucket] = errors.New("delete failed")
}

func prepareSettlementSequenceMismatch(t *testing.T, fixture *scriptedQueueFixture) {
	putUnmigratedLeaseSettlement(t, fixture.queue, "lease")
	if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := fixture.queue.leaseSettlements.Put(tx, vault.Key("lease"), leaseSettlementRecord{
			Outcome:  leaseSettlementAcknowledged,
			Sequence: 1,
		}); err != nil {
			return fmt.Errorf("replace mismatched settlement: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("replace settlement: %v", err)
	}
}

func prepareSettlementTimeFailure(t *testing.T, fixture *scriptedQueueFixture) {
	putUnmigratedLeaseSettlement(t, fixture.queue, "lease")
	fixture.engine.putErrors[leaseSettlementBucket] = errors.New("put failed")
}

func prepareSettlementExpiryIndexFailure(t *testing.T, fixture *scriptedQueueFixture) {
	putUnmigratedLeaseSettlement(t, fixture.queue, "lease")
	fixture.engine.putErrors[leaseSettlementExpiryBucket] = errors.New("put failed")
}

func prepareSettlementCursorAdvanceFailure(t *testing.T, fixture *scriptedQueueFixture) {
	putUnmigratedLeaseSettlement(t, fixture.queue, "lease")
	fixture.engine.putKeyErrors[seqBucket] = map[string]error{
		string(leaseSettlementMigrationNextKey): errors.New("put failed"),
	}
}

type leaseSettlementExpiryFailure struct {
	name    string
	prepare func(*testing.T, *scriptedQueueFixture, string, vault.Key)
}

func leaseSettlementExpiryFailures() []leaseSettlementExpiryFailure {
	return []leaseSettlementExpiryFailure{
		{"expiry scan", prepareSettlementExpiryScanFailure},
		{"settlement record", prepareInvalidExpiringSettlementRecord},
		{"stale expiry delete", prepareStaleSettlementExpiryDeleteFailure},
		{"active lease read", prepareActiveLeaseReadFailure},
		{"order index read", prepareSettlementOrderReadFailure},
		{"order index mismatch", prepareSettlementOrderMismatch},
		{"settlement delete", prepareSettlementDeleteFailure},
		{"order index delete", prepareSettlementOrderDeleteFailure},
		{"completed control delete", prepareCompletedControlDeleteFailure},
		{"expiry delete", prepareSettlementExpiryDeleteFailure},
	}
}

func TestLeaseSettlementExpirySurfacesStorageFailures(t *testing.T) {
	set := withClock(t)
	base := time.Unix(110_000, 0)
	set(base)
	for _, test := range leaseSettlementExpiryFailures() {
		t.Run(test.name, func(t *testing.T) {
			fixture, leaseID, expiryKey := scriptedExpiredLeaseSettlement(t, base)
			test.prepare(t, &fixture, leaseID, expiryKey)
			if err := fixture.queue.deleteExpiredLeaseSettlements(
				t.Context(),
				base.Add(leaseSettlementRetention),
			); err == nil {
				t.Fatal("expiry storage failure was hidden")
			}
		})
	}
}

func prepareSettlementExpiryScanFailure(
	_ *testing.T,
	fixture *scriptedQueueFixture,
	_ string,
	_ vault.Key,
) {
	fixture.engine.scanErrors[leaseSettlementExpiryBucket] = errors.New("scan failed")
}

func prepareInvalidExpiringSettlementRecord(
	_ *testing.T,
	fixture *scriptedQueueFixture,
	leaseID string,
	_ vault.Key,
) {
	fixture.engine.buckets[leaseSettlementBucket][leaseID] = []byte{1}
}

func prepareStaleSettlementExpiryDeleteFailure(
	_ *testing.T,
	fixture *scriptedQueueFixture,
	leaseID string,
	_ vault.Key,
) {
	delete(fixture.engine.buckets[leaseSettlementBucket], leaseID)
	fixture.engine.deleteErrors[leaseSettlementExpiryBucket] = errors.New("delete failed")
}

func prepareActiveLeaseReadFailure(
	_ *testing.T,
	fixture *scriptedQueueFixture,
	leaseID string,
	_ vault.Key,
) {
	fixture.engine.buckets[leaseBucket][leaseID] = []byte("{")
}

func prepareSettlementOrderReadFailure(
	_ *testing.T,
	fixture *scriptedQueueFixture,
	_ string,
	_ vault.Key,
) {
	fixture.engine.buckets[leaseSettlementOrderBucket][string(orderKey(0))] = []byte{0}
}

func prepareSettlementOrderMismatch(
	_ *testing.T,
	fixture *scriptedQueueFixture,
	_ string,
	_ vault.Key,
) {
	raw, _ := (leaseSettlementIdentityCodec{}).Encode([]byte("other"))
	fixture.engine.buckets[leaseSettlementOrderBucket][string(orderKey(0))] = raw
}

func prepareSettlementDeleteFailure(
	_ *testing.T,
	fixture *scriptedQueueFixture,
	_ string,
	_ vault.Key,
) {
	fixture.engine.deleteErrors[leaseSettlementBucket] = errors.New("delete failed")
}

func prepareSettlementOrderDeleteFailure(
	_ *testing.T,
	fixture *scriptedQueueFixture,
	_ string,
	_ vault.Key,
) {
	fixture.engine.deleteErrors[leaseSettlementOrderBucket] = errors.New("delete failed")
}

func prepareCompletedControlDeleteFailure(
	t *testing.T,
	fixture *scriptedQueueFixture,
	leaseID string,
	_ vault.Key,
) {
	if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		target := leaseControlTarget{WorkerID: "worker", RunID: "run"}
		if err := fixture.queue.completedControlTargets.Put(
			tx,
			vault.Key(leaseID),
			target,
		); err != nil {
			return fmt.Errorf("store completed control target: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("put completed control: %v", err)
	}
	fixture.engine.deleteErrors[completedLeaseControlTargetBucket] = errors.New("delete failed")
}

func prepareSettlementExpiryDeleteFailure(
	_ *testing.T,
	fixture *scriptedQueueFixture,
	_ string,
	_ vault.Key,
) {
	fixture.engine.deleteErrors[leaseSettlementExpiryBucket] = errors.New("delete failed")
}

func TestLeaseSettlementExpiryRejectsMalformedIndex(t *testing.T) {
	fixture := scriptedQueue(t)
	value, _ := (leaseSettlementIdentityCodec{}).Encode([]byte("lease"))
	fixture.engine.buckets[leaseSettlementExpiryBucket]["short"] = value
	if err := fixture.queue.deleteExpiredLeaseSettlements(t.Context(), time.Now()); err == nil {
		t.Fatal("malformed expiry index was accepted")
	}
	delete(fixture.engine.buckets[leaseSettlementExpiryBucket], "short")
	fixture.engine.buckets[leaseSettlementExpiryBucket][string(make([]byte, 16))] = value
	if err := fixture.queue.deleteExpiredLeaseSettlements(t.Context(), time.Now()); err == nil {
		t.Fatal("zero expiry index was accepted")
	}
}

func TestLeaseSettlementExpiryDropsStaleIndex(t *testing.T) {
	set := withClock(t)
	base := time.Unix(115_000, 0)
	set(base)
	fixture, leaseID, _ := scriptedExpiredLeaseSettlement(t, base)
	if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		record, found, err := fixture.queue.leaseSettlements.Get(tx, vault.Key(leaseID))
		if err != nil {
			return fmt.Errorf("read settlement before stale-index preparation: %w", err)
		}
		if !found {
			return fmt.Errorf("settlement is missing")
		}
		if _, err := fixture.queue.leaseSettlements.Delete(tx, vault.Key(leaseID)); err != nil {
			return fmt.Errorf("delete settlement before stale-index preparation: %w", err)
		}
		_, err = fixture.queue.leaseSettlementOrder.Delete(tx, orderKey(record.Sequence))
		if err != nil {
			return fmt.Errorf("delete settlement order before stale-index preparation: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("delete settlement: %v", err)
	}
	if err := fixture.queue.expireLeaseSettlements(
		t.Context(),
		base.Add(leaseSettlementRetention),
	); err != nil {
		t.Fatalf("drop stale expiry: %v", err)
	}
	assertLeaseSettlementRetentionCounts(t, fixture.queue, leaseSettlementRows{})
}

func TestLeaseSettlementExpiryStopsAtFutureEntry(t *testing.T) {
	set := withClock(t)
	base := time.Unix(117_000, 0)
	set(base)
	queue := memQueue(t)
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return queue.recordLeaseSettlement(tx, "expired", leaseSettlementAcknowledged)
	}); err != nil {
		t.Fatalf("record expired settlement: %v", err)
	}
	set(base.Add(time.Hour))
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return queue.recordLeaseSettlement(tx, "future", leaseSettlementAcknowledged)
	}); err != nil {
		t.Fatalf("record future settlement: %v", err)
	}
	if err := queue.deleteExpiredLeaseSettlements(
		t.Context(),
		base.Add(leaseSettlementRetention),
	); err != nil {
		t.Fatalf("expire due settlement: %v", err)
	}
	assertLeaseSettlementRetentionCounts(t, queue, leaseSettlementRows{1, 1, 1, 0, 0})
}

func TestLeaseSettlementExpiryRejectsMalformedEntryAfterDueRow(t *testing.T) {
	set := withClock(t)
	base := time.Unix(118_000, 0)
	set(base)
	fixture, _, expiryKey := scriptedExpiredLeaseSettlement(t, base)
	malformed := append(append([]byte(nil), expiryKey...), 0)
	value, _ := (leaseSettlementIdentityCodec{}).Encode([]byte("malformed"))
	fixture.engine.buckets[leaseSettlementExpiryBucket][string(malformed)] = value
	if err := fixture.queue.deleteExpiredLeaseSettlements(
		t.Context(),
		base.Add(leaseSettlementRetention),
	); err == nil {
		t.Fatal("malformed expiry after due row was accepted")
	}
}

func TestLeaseSettlementExpiryWaitsForActiveLease(t *testing.T) {
	set := withClock(t)
	base := time.Unix(120_000, 0)
	set(base)
	fixture, leaseID, _ := scriptedExpiredLeaseSettlement(t, base)
	if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := fixture.queue.leases.Put(tx, vault.Key(leaseID), leaseRecord{
			OrderData:         []byte("order"),
			WorkerID:          "worker",
			ExpiresAtUnixNano: base.Add(time.Hour).UnixNano(),
		}); err != nil {
			return fmt.Errorf("store active lease: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("put active lease: %v", err)
	}
	if err := fixture.queue.deleteExpiredLeaseSettlements(
		t.Context(),
		base.Add(leaseSettlementRetention),
	); err != nil {
		t.Fatalf("retain active lease settlement: %v", err)
	}
	assertLeaseSettlementRetentionCounts(t, fixture.queue, leaseSettlementRows{1, 1, 1, 0, 0})
}

func putUnmigratedLeaseSettlement(t *testing.T, queue *DurableOrderQueue, leaseID string) {
	t.Helper()
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := queue.leaseSettlements.Put(tx, vault.Key(leaseID), leaseSettlementRecord{
			Outcome: leaseSettlementAcknowledged,
		}); err != nil {
			return fmt.Errorf("store unmigrated settlement: %w", err)
		}
		if err := queue.leaseSettlementOrder.Put(tx, orderKey(0), []byte(leaseID)); err != nil {
			return fmt.Errorf("store unmigrated settlement order: %w", err)
		}

		if err := queue.seq.Put(tx, leaseSettlementNextKey, 1); err != nil {
			return fmt.Errorf("store unmigrated settlement sequence: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("put unmigrated settlement: %v", err)
	}
}

func scriptedExpiredLeaseSettlement(
	t *testing.T,
	settledAt time.Time,
) (scriptedQueueFixture, string, vault.Key) {
	t.Helper()
	fixture := scriptedQueue(t)
	leaseID := "lease"
	if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return fixture.queue.recordLeaseSettlement(
			tx,
			leaseID,
			leaseSettlementAcknowledged,
		)
	}); err != nil {
		t.Fatalf("record expired settlement: %v", err)
	}
	record := leaseSettlementRecord{}
	if err := fixture.queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		var found bool
		var err error
		record, found, err = fixture.queue.leaseSettlements.Get(tx, vault.Key(leaseID))
		if err != nil {
			return fmt.Errorf("read expired settlement: %w", err)
		}
		if !found {
			return fmt.Errorf("settlement is missing")
		}

		return nil
	}); err != nil {
		t.Fatalf("read expired settlement: %v", err)
	}
	if record.SettledAtUnixNano != settledAt.UnixNano() {
		t.Fatalf("settled at = %d, want %d", record.SettledAtUnixNano, settledAt.UnixNano())
	}

	return fixture, leaseID, leaseSettlementExpiryKey(record)
}

type leaseSettlementRows struct {
	settlements       int
	orderEntries      int
	expiryEntries     int
	completedControls int
	pendingControls   int
}

func assertLeaseSettlementRetentionCounts(
	t *testing.T,
	queue *DurableOrderQueue,
	want leaseSettlementRows,
) {
	t.Helper()
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		actualSettlements, err := queue.leaseSettlements.Len(tx)
		if err != nil {
			return fmt.Errorf("count settlements: %w", err)
		}
		actualOrderEntries, err := queue.leaseSettlementOrder.Len(tx)
		if err != nil {
			return fmt.Errorf("count settlement order entries: %w", err)
		}
		actualExpiryEntries, err := queue.leaseSettlementExpiry.Len(tx)
		if err != nil {
			return fmt.Errorf("count settlement expiry entries: %w", err)
		}
		actualCompletedControls, err := queue.completedControlTargets.Len(tx)
		if err != nil {
			return fmt.Errorf("count completed control targets: %w", err)
		}
		actualPendingControls, err := queue.leaseControlTargets.Len(tx)
		if err != nil {
			return fmt.Errorf("count pending control targets: %w", err)
		}
		if actualSettlements != want.settlements || actualOrderEntries != want.orderEntries ||
			actualExpiryEntries != want.expiryEntries ||
			actualCompletedControls != want.completedControls ||
			actualPendingControls != want.pendingControls {
			t.Fatalf(
				"retention rows = settlements %d, order %d, expiry %d, completed %d, pending %d",
				actualSettlements,
				actualOrderEntries,
				actualExpiryEntries,
				actualCompletedControls,
				actualPendingControls,
			)
		}

		return nil
	}); err != nil {
		t.Fatalf("read retention rows: %v", err)
	}
}
