package crawlbroker

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestLeaseSettlementCodecRejectsEveryInvalidRecordFamily(t *testing.T) {
	invalidLegacy := make([]byte, 9)
	if _, err := (leaseSettlementRecordCodec{}).Decode(invalidLegacy); err == nil {
		t.Fatal("legacy settlement with invalid outcome decoded")
	}
	if _, err := (leaseSettlementRecordCodec{}).Decode([]byte{
		leaseSettlementRecordFormat,
		'{',
	}); err == nil {
		t.Fatal("malformed settlement JSON decoded")
	}
	invalidOutcome, err := (leaseSettlementRecordCodec{}).Encode(leaseSettlementRecord{})
	if err != nil {
		t.Fatalf("encode invalid outcome fixture: %v", err)
	}
	if _, err := (leaseSettlementRecordCodec{}).Decode(invalidOutcome); err == nil {
		t.Fatal("settlement with invalid outcome decoded")
	}
	if err := validateLeaseSettlementRecord(leaseSettlementRecord{
		Outcome:           leaseSettlementAcknowledged,
		ProgressDelivered: true,
	}); err == nil {
		t.Fatal("legacy settlement with terminal fields validated")
	}
	if err := validateLeaseSettlementRecord(leaseSettlementRecord{
		Outcome:  leaseSettlementAcknowledged,
		Terminal: true,
	}); err == nil {
		t.Fatal("terminal settlement without identity validated")
	}
}

func TestLegacySettlementTimeMigrationSurfacesBothWrites(t *testing.T) {
	for _, test := range []struct {
		name   string
		bucket vault.Name
	}{
		{name: "record", bucket: leaseSettlementBucket},
		{name: "expiry", bucket: leaseSettlementExpiryBucket},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := scriptedQueue(t)
			putSettlementRaw(t, &fixture, "lease", leaseSettlementRecord{
				Outcome:  leaseSettlementAcknowledged,
				Sequence: 1,
			})
			fixture.engine.putErrors[test.bucket] = errors.New("write failed")
			err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
				return fixture.queue.requireLeaseSettlement(
					tx,
					"lease",
					leaseSettlementAcknowledged,
				)
			})
			if err == nil {
				t.Fatal("legacy migration write failure was hidden")
			}
		})
	}
}

func TestTerminalSettlementReadAndExistingConflictAreRejected(t *testing.T) {
	t.Run("read", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.buckets[leaseSettlementBucket]["lease"] = []byte("{")
		err := fixture.queue.vault.View(t.Context(), func(tx *vault.Txn) error {
			_, err := fixture.queue.requireTerminalLeaseSettlement(
				tx,
				"lease",
				validTerminalSettlementRecord(),
			)

			return err
		})
		if err == nil {
			t.Fatal("terminal settlement read failure was hidden")
		}
	})

	t.Run("existing conflict", func(t *testing.T) {
		queue := memQueue(t)
		err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			first := validTerminalSettlementRecord()
			if _, err := queue.storeLeaseSettlement(tx, "lease", first); err != nil {
				return err
			}
			second := first
			second.Progress.State = yagocrawlcontract.CrawlRunCancelled
			_, err := queue.storeLeaseSettlement(tx, "lease", second)

			return err
		})
		if !errors.Is(err, errLeaseDispositionConflict) {
			t.Fatalf("terminal settlement conflict = %v", err)
		}
	})
}

func TestSettlementMigrationHandlesMissingIndex(t *testing.T) {
	queue := memQueue(t)
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return queue.migrateLeaseSettlementAtSequence(tx, 1, time.Now())
	}); err != nil {
		t.Fatalf("missing index migration: %v", err)
	}
}

func TestSettlementMigrationHandlesOrphanIndex(t *testing.T) {
	queue := memQueue(t)
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := queue.leaseSettlementOrder.Put(tx, orderKey(1), []byte("lease")); err != nil {
			return fmt.Errorf("store orphan migration index: %w", err)
		}

		return queue.migrateLeaseSettlementAtSequence(tx, 1, time.Now())
	}); err != nil {
		t.Fatalf("orphan migration: %v", err)
	}
}

func TestSettlementMigrationHandlesTimestampedRecord(t *testing.T) {
	queue := memQueue(t)
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := queue.leaseSettlementOrder.Put(tx, orderKey(1), []byte("lease")); err != nil {
			return fmt.Errorf("store timestamped migration index: %w", err)
		}
		if err := queue.leaseSettlements.Put(tx, vault.Key("lease"), leaseSettlementRecord{
			Outcome: leaseSettlementAcknowledged, Sequence: 1, SettledAtUnixNano: 1,
		}); err != nil {
			return fmt.Errorf("store timestamped migration record: %w", err)
		}

		return queue.migrateLeaseSettlementAtSequence(tx, 1, time.Now())
	}); err != nil {
		t.Fatalf("timestamped migration: %v", err)
	}
}

func TestSettlementMigrationHandlesTerminalRecord(t *testing.T) {
	queue := memQueue(t)
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		record := validTerminalSettlementRecord()
		record.Sequence = 1
		if err := queue.leaseSettlementOrder.Put(tx, orderKey(1), []byte("lease")); err != nil {
			return fmt.Errorf("store terminal migration index: %w", err)
		}
		if err := queue.leaseSettlements.Put(tx, vault.Key("lease"), record); err != nil {
			return fmt.Errorf("store terminal migration record: %w", err)
		}

		return queue.migrateLeaseSettlementAtSequence(tx, 1, time.Now())
	}); err != nil {
		t.Fatalf("terminal migration: %v", err)
	}
}

func TestSettlementExpiryIdentityRejectsNonpositiveTimestamp(t *testing.T) {
	if key := leaseSettlementExpiryKey(leaseSettlementRecord{}); key != nil {
		t.Fatalf("nonpositive settlement expiry key = %x", key)
	}
}

func validTerminalSettlementRecord() leaseSettlementRecord {
	return leaseSettlementRecord{
		Outcome:         leaseSettlementAcknowledged,
		OrderIdentity:   make([]byte, 32),
		WorkerSessionID: testWorkerSessionID,
		Progress: yagocrawlcontract.CrawlRunProgress{
			RunID: "run", WorkerID: "worker", State: yagocrawlcontract.CrawlRunFinished,
		},
		Terminal: true,
	}
}

func putSettlementRaw(
	t *testing.T,
	fixture *scriptedQueueFixture,
	leaseID string,
	record leaseSettlementRecord,
) {
	t.Helper()
	raw, err := (leaseSettlementRecordCodec{}).Encode(record)
	if err != nil {
		t.Fatalf("encode settlement fixture: %v", err)
	}
	fixture.engine.buckets[leaseSettlementBucket][leaseID] = raw
}
