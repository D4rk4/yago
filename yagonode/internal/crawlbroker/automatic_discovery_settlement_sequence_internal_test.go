package crawlbroker

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestAutomaticDiscoverySettlementRelocatesInterleavedSequenceCollision(
	t *testing.T,
) {
	for _, terminal := range []bool{false, true} {
		disposition := "acknowledgment"
		if terminal {
			disposition = "terminal"
		}
		for _, recovery := range []string{"retry", "reopen"} {
			t.Run(disposition+"/"+recovery, func(t *testing.T) {
				runAutomaticDiscoverySettlementSequenceCollision(
					t,
					terminal,
					recovery,
				)
			})
		}
	}
}

func runAutomaticDiscoverySettlementSequenceCollision(
	t *testing.T,
	terminal bool,
	recovery string,
) {
	t.Helper()
	fixture := scriptedQueue(t)
	target := fmt.Sprintf(
		"https://settlement-sequence-%t-%s.example/",
		terminal,
		recovery,
	)
	data, leaseID := automaticDiscoverySettlementLease(t, fixture.queue, target)
	stageAutomaticDiscoverySettlement(t, fixture.queue, data, leaseID, terminal)
	if err := seedAutomaticDiscoverySettlementRecord(
		t,
		&fixture,
		leaseID,
		0,
		false,
	); err != nil {
		t.Fatal(err)
	}
	otherLeaseID := settleInterleavedLease(t, fixture.queue)
	recovered := recoverCollidedAutomaticDiscoverySettlement(t, fixture, settlementRecoveryCase{
		terminal: terminal,
		method:   recovery,
		data:     data,
		leaseID:  leaseID,
	})
	requireAutomaticDiscoverySettlementComplete(
		t,
		recovered,
		target,
		leaseID,
		terminal,
	)
	requireInterleavedSettlementSequences(t, recovered, leaseID, otherLeaseID)
}

func stageAutomaticDiscoverySettlement(
	t *testing.T,
	queue *DurableOrderQueue,
	data []byte,
	leaseID string,
	terminal bool,
) {
	t.Helper()
	if terminal {
		if err := queue.stageAutomaticDiscoveryTerminalSettlement(
			t.Context(),
			leaseID,
			automaticDiscoveryTerminalRequest(data),
		); err != nil {
			t.Fatalf("stage terminal settlement: %v", err)
		}
	} else if err := queue.stageAutomaticDiscoveryAcknowledgment(
		t.Context(),
		leaseID,
		"worker",
		"session",
		true,
	); err != nil {
		t.Fatalf("stage acknowledgment: %v", err)
	}
}

func settleInterleavedLease(t *testing.T, queue *DurableOrderQueue) string {
	t.Helper()
	if err := queue.Publish(t.Context(), testOrder("interleaved-settlement")); err != nil {
		t.Fatalf("publish interleaved order: %v", err)
	}
	_, leaseID, found, err := queue.leasePopForSession(
		t.Context(),
		"other-worker",
		"other-session",
	)
	if err != nil || !found {
		t.Fatalf("lease interleaved order = %t, %v", found, err)
	}
	if _, err := queue.ackLeaseWithOwner(
		t.Context(),
		leaseID,
		"other-worker",
		"other-session",
	); err != nil {
		t.Fatalf("settle interleaved order: %v", err)
	}

	return leaseID
}

type settlementRecoveryCase struct {
	terminal bool
	method   string
	data     []byte
	leaseID  string
}

func recoverCollidedAutomaticDiscoverySettlement(
	t *testing.T,
	fixture scriptedQueueFixture,
	recovery settlementRecoveryCase,
) *DurableOrderQueue {
	t.Helper()
	recovered := fixture.queue
	if recovery.method == "retry" {
		if err := settleAutomaticDiscoveryLease(
			t,
			recovered,
			recovery.terminal,
			recovery.data,
			recovery.leaseID,
		); err != nil {
			t.Fatalf("retry collided settlement: %v", err)
		}
	} else {
		recovered = reopenAutomaticDiscoveryQueue(t, fixture.engine)
	}

	return recovered
}

func requireInterleavedSettlementSequences(
	t *testing.T,
	queue *DurableOrderQueue,
	automaticLeaseID string,
	otherLeaseID string,
) {
	t.Helper()
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		automatic, found, err := queue.leaseSettlements.Get(
			tx,
			vault.Key(automaticLeaseID),
		)
		if err != nil || !found {
			return fmt.Errorf("read automatic settlement: %w", err)
		}
		other, found, err := queue.leaseSettlements.Get(tx, vault.Key(otherLeaseID))
		if err != nil || !found {
			return fmt.Errorf("read interleaved settlement: %w", err)
		}
		automaticIndex, found, err := queue.leaseSettlementOrder.Get(
			tx,
			orderKey(automatic.Sequence),
		)
		if err != nil || !found {
			return fmt.Errorf("read automatic settlement index: %w", err)
		}
		otherIndex, found, err := queue.leaseSettlementOrder.Get(
			tx,
			orderKey(other.Sequence),
		)
		if err != nil || !found {
			return fmt.Errorf("read interleaved settlement index: %w", err)
		}
		next, _, err := queue.seq.Get(tx, leaseSettlementNextKey)
		if err != nil {
			return fmt.Errorf("read settlement sequence: %w", err)
		}
		if automatic.Sequence == other.Sequence ||
			string(automaticIndex) != automaticLeaseID ||
			string(otherIndex) != otherLeaseID ||
			next <= automatic.Sequence ||
			next <= other.Sequence {
			return fmt.Errorf(
				"settlement identities = automatic %d/%q, interleaved %d/%q, next %d",
				automatic.Sequence,
				automaticIndex,
				other.Sequence,
				otherIndex,
				next,
			)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLeaseSettlementSequenceReservationSkipsOccupiedIdentity(t *testing.T) {
	fixture := scriptedQueue(t)
	if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := fixture.queue.leaseSettlementOrder.Put(
			tx,
			orderKey(0),
			[]byte("existing"),
		); err != nil {
			return fmt.Errorf("seed occupied settlement identity: %w", err)
		}
		record, err := fixture.queue.storeLeaseSettlement(
			tx,
			"new",
			leaseSettlementRecord{Outcome: leaseSettlementAcknowledged},
		)
		if err != nil {
			return fmt.Errorf("store settlement after occupied identity: %w", err)
		}
		if record.Sequence != 1 {
			return fmt.Errorf("reserved settlement sequence = %d", record.Sequence)
		}

		return nil
	}); err != nil {
		t.Fatalf("reserve settlement sequence: %v", err)
	}
	if err := fixture.queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		existing, _, err := fixture.queue.leaseSettlementOrder.Get(tx, orderKey(0))
		if err != nil {
			return fmt.Errorf("read occupied settlement identity: %w", err)
		}
		added, _, err := fixture.queue.leaseSettlementOrder.Get(tx, orderKey(1))
		if err != nil {
			return fmt.Errorf("read allocated settlement identity: %w", err)
		}
		next, _, err := fixture.queue.seq.Get(tx, leaseSettlementNextKey)
		if err != nil {
			return fmt.Errorf("read reserved settlement sequence: %w", err)
		}
		if string(existing) != "existing" || string(added) != "new" || next != 2 {
			return fmt.Errorf(
				"settlement reservation = %q, %q, next %d",
				existing,
				added,
				next,
			)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLeaseSettlementSequenceReservationSurfacesIndexReadFailure(t *testing.T) {
	fixture := scriptedQueue(t)
	fixture.engine.readErrors[leaseSettlementOrderBucket] = errors.New("read failed")
	if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		_, err := fixture.queue.storeLeaseSettlement(
			tx,
			"lease",
			leaseSettlementRecord{Outcome: leaseSettlementAcknowledged},
		)
		if err != nil {
			return fmt.Errorf("store settlement with unreadable index: %w", err)
		}

		return nil
	}); err == nil {
		t.Fatal("settlement sequence index read failure was hidden")
	}
}

func TestAutomaticDiscoverySettlementSequenceRelocationSurfacesFailures(
	t *testing.T,
) {
	for _, test := range []struct {
		name    string
		prepare func(*scriptedQueueFixture)
	}{
		{
			name: "reservation",
			prepare: func(fixture *scriptedQueueFixture) {
				fixture.engine.buckets[seqBucket][string(leaseSettlementNextKey)] = []byte{1}
			},
		},
		{
			name: "expiry read",
			prepare: func(fixture *scriptedQueueFixture) {
				fixture.engine.readErrors[leaseSettlementExpiryBucket] = fmt.Errorf("read failed")
			},
		},
		{
			name: "expiry delete",
			prepare: func(fixture *scriptedQueueFixture) {
				fixture.engine.deleteErrors[leaseSettlementExpiryBucket] = fmt.Errorf(
					"delete failed",
				)
			},
		},
		{
			name: "settlement update",
			prepare: func(fixture *scriptedQueueFixture) {
				fixture.engine.putErrors[leaseSettlementBucket] = fmt.Errorf("put failed")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, leaseID := collidedAutomaticDiscoverySettlement(t)
			test.prepare(&fixture)
			if _, err := fixture.queue.completeAutomaticDiscoverySettlement(
				t.Context(),
				leaseID,
			); err == nil {
				t.Fatal("settlement relocation failure was hidden")
			}
		})
	}
}

func TestAutomaticDiscoverySettlementSequenceFloorSurfacesFailures(t *testing.T) {
	for _, key := range []vault.Key{
		leaseSettlementNextKey,
		leaseSettlementMigrationNextKey,
	} {
		t.Run(string(key), func(t *testing.T) {
			fixture := scriptedQueue(t)
			target := "https://settlement-floor.example/"
			_, leaseID := automaticDiscoverySettlementLease(t, fixture.queue, target)
			if err := fixture.queue.stageAutomaticDiscoveryAcknowledgment(
				t.Context(),
				leaseID,
				"worker",
				"session",
				true,
			); err != nil {
				t.Fatalf("stage acknowledgment: %v", err)
			}
			if err := seedAutomaticDiscoverySettlementRecord(
				t,
				&fixture,
				leaseID,
				7,
				false,
			); err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(key, leaseSettlementMigrationNextKey) {
				raw, _ := (sequenceCodec{}).Encode(8)
				fixture.engine.buckets[seqBucket][string(leaseSettlementNextKey)] = raw
			}
			fixture.engine.putKeyErrors[seqBucket] = map[string]error{
				string(key): fmt.Errorf("put failed"),
			}
			if _, err := fixture.queue.completeAutomaticDiscoverySettlement(
				t.Context(),
				leaseID,
			); err == nil {
				t.Fatal("settlement sequence floor failure was hidden")
			}
		})
	}
}

func collidedAutomaticDiscoverySettlement(
	t *testing.T,
) (scriptedQueueFixture, string) {
	t.Helper()
	fixture := scriptedQueue(t)
	target := "https://collided-settlement.example/"
	_, leaseID := automaticDiscoverySettlementLease(t, fixture.queue, target)
	if err := fixture.queue.stageAutomaticDiscoveryAcknowledgment(
		t.Context(),
		leaseID,
		"worker",
		"session",
		true,
	); err != nil {
		t.Fatalf("stage acknowledgment: %v", err)
	}
	if err := seedAutomaticDiscoverySettlementRecord(
		t,
		&fixture,
		leaseID,
		0,
		true,
	); err != nil {
		t.Fatal(err)
	}

	return fixture, leaseID
}

func seedAutomaticDiscoverySettlementRecord(
	t *testing.T,
	fixture *scriptedQueueFixture,
	leaseID string,
	sequence uint64,
	collide bool,
) error {
	t.Helper()

	if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		intent, found, err := fixture.queue.discoverySettlements.Get(
			tx,
			vault.Key(leaseID),
		)
		if err != nil {
			return fmt.Errorf("read staged settlement: %w", err)
		}
		if !found {
			return fmt.Errorf("staged settlement is missing")
		}
		record := intent.Settlement
		record.Sequence = sequence
		record.SettledAtUnixNano = nowFunc().UnixNano()
		if err := fixture.queue.leaseSettlements.Put(
			tx,
			vault.Key(leaseID),
			record,
		); err != nil {
			return fmt.Errorf("seed partial settlement: %w", err)
		}
		if !record.Terminal {
			if err := fixture.queue.leaseSettlementExpiry.Put(
				tx,
				leaseSettlementExpiryKey(record),
				[]byte(leaseID),
			); err != nil {
				return fmt.Errorf("seed partial settlement expiry: %w", err)
			}
		}
		if !collide {
			return nil
		}

		if err := fixture.queue.leaseSettlementOrder.Put(
			tx,
			orderKey(sequence),
			[]byte("other"),
		); err != nil {
			return fmt.Errorf("seed conflicting settlement identity: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("seed automatic discovery settlement record: %w", err)
	}

	return nil
}
