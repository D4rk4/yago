package crawlbroker

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// TestLeaseSettlementMigrationLeavesTimestampedRecordsAlone pins the idempotence
// arm of the retention migration. The migration cursor is a single watermark over
// the whole settlement sequence, so a node that upgraded mid-flight sweeps a range
// holding both legacy rows with no settlement time and rows a current node already
// stamped. Restamping an already-correct row would reset its retention horizon
// from the moment it was settled to the moment the sweep happened, keeping a
// settlement that should be ageing out alive for another full retention window,
// and would file a second expiry index row under the new time while the original
// stays behind. Only the legacy row in the same range may change.
func TestLeaseSettlementMigrationLeavesTimestampedRecordsAlone(t *testing.T) {
	set := withClock(t)
	settledAt := time.Unix(200_000, 0)
	sweptAt := settledAt.Add(time.Hour)
	set(sweptAt)
	queue := memQueue(t)
	timestamped := leaseSettlementRecord{
		Outcome:           leaseSettlementAcknowledged,
		Sequence:          0,
		SettledAtUnixNano: settledAt.UnixNano(),
	}
	putMixedVersionLeaseSettlements(t, queue, timestamped)
	if err := queue.expireLeaseSettlements(t.Context(), sweptAt); err != nil {
		t.Fatalf("migrate mixed-version settlements: %v", err)
	}
	migrated := leaseSettlementFor(t, queue, "timestamped")
	if migrated.SettledAtUnixNano != timestamped.SettledAtUnixNano {
		t.Fatalf("already-correct settlement was restamped to %d, want its own time %d",
			migrated.SettledAtUnixNano, timestamped.SettledAtUnixNano)
	}
	if !reflect.DeepEqual(migrated, timestamped) {
		t.Fatal("migration altered an already-correct settlement")
	}
	if got := leaseSettlementFor(t, queue, "legacy"); got.SettledAtUnixNano != sweptAt.UnixNano() {
		t.Fatalf("legacy settlement settled at %d, want the sweep time %d",
			got.SettledAtUnixNano, sweptAt.UnixNano())
	}
	assertLeaseSettlementRetentionCounts(t, queue, leaseSettlementRows{
		settlements:   2,
		orderEntries:  2,
		expiryEntries: 2,
	})
	set(settledAt.Add(leaseSettlementRetention))
	if err := queue.expireLeaseSettlements(
		t.Context(),
		settledAt.Add(leaseSettlementRetention),
	); err != nil {
		t.Fatalf("expire the timestamped settlement: %v", err)
	}
	if _, found := lookupLeaseSettlement(t, queue, "timestamped"); found {
		t.Fatal("already-correct settlement outlived its own retention horizon")
	}
}

func putMixedVersionLeaseSettlements(
	t *testing.T,
	queue *DurableOrderQueue,
	timestamped leaseSettlementRecord,
) {
	t.Helper()
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := queue.leaseSettlements.Put(
			tx,
			vault.Key("timestamped"),
			timestamped,
		); err != nil {
			return fmt.Errorf("store timestamped settlement: %w", err)
		}
		if err := queue.leaseSettlementExpiry.Put(
			tx,
			leaseSettlementExpiryKey(timestamped),
			[]byte("timestamped"),
		); err != nil {
			return fmt.Errorf("store timestamped settlement expiry: %w", err)
		}
		if err := queue.leaseSettlementOrder.Put(
			tx,
			orderKey(timestamped.Sequence),
			[]byte("timestamped"),
		); err != nil {
			return fmt.Errorf("store timestamped settlement order: %w", err)
		}
		if err := queue.leaseSettlements.Put(tx, vault.Key("legacy"), leaseSettlementRecord{
			Outcome:  leaseSettlementAcknowledged,
			Sequence: timestamped.Sequence + 1,
		}); err != nil {
			return fmt.Errorf("store legacy settlement: %w", err)
		}
		if err := queue.leaseSettlementOrder.Put(
			tx,
			orderKey(timestamped.Sequence+1),
			[]byte("legacy"),
		); err != nil {
			return fmt.Errorf("store legacy settlement order: %w", err)
		}
		if err := queue.seq.Put(
			tx,
			leaseSettlementNextKey,
			timestamped.Sequence+2,
		); err != nil {
			return fmt.Errorf("store mixed-version settlement sequence: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("put mixed-version settlements: %v", err)
	}
}

func leaseSettlementFor(
	t *testing.T,
	queue *DurableOrderQueue,
	leaseID string,
) leaseSettlementRecord {
	t.Helper()
	record, found := lookupLeaseSettlement(t, queue, leaseID)
	if !found {
		t.Fatalf("settlement %q not found", leaseID)
	}

	return record
}

func lookupLeaseSettlement(
	t *testing.T,
	queue *DurableOrderQueue,
	leaseID string,
) (leaseSettlementRecord, bool) {
	t.Helper()
	var record leaseSettlementRecord
	found := false
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		var err error
		record, found, err = queue.leaseSettlements.Get(tx, vault.Key(leaseID))
		if err != nil {
			return fmt.Errorf("read settlement %q: %w", leaseID, err)
		}

		return nil
	}); err != nil {
		t.Fatalf("read settlement: %v", err)
	}

	return record, found
}
