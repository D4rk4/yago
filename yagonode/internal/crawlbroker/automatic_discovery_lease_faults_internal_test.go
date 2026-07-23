package crawlbroker

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func pendingAutomaticDiscoveryLease(
	t *testing.T,
	fixture scriptedQueueFixture,
	target string,
	key vault.Key,
	sidecar bool,
) (pendingOrderLease, leaseRecord) {
	t.Helper()
	data := automaticDiscoveryData(t, target)
	if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := fixture.queue.orders.Put(tx, key, data); err != nil {
			return fmt.Errorf("write pending order: %w", err)
		}
		if err := fixture.queue.automaticOrderIndex.Put(
			tx,
			key,
			priorityIndexMarker,
		); err != nil {
			return fmt.Errorf("index pending order: %w", err)
		}
		if !sidecar {
			return nil
		}

		return fixture.queue.pendingDiscoveryKeys.Put(tx, key, []byte(target))
	}); err != nil {
		t.Fatalf("seed pending automatic discovery: %v", err)
	}
	lease := pendingOrderLease{
		order: pendingOrderHead{
			index:     fixture.queue.automaticOrderIndex,
			key:       key,
			data:      data,
			found:     true,
			automatic: true,
		},
		leaseID:         "lease",
		workerID:        "worker",
		workerSessionID: "session",
		leasedAt:        time.Unix(1, 0),
	}

	return lease, lease.record(DefaultLeaseTTL)
}

func TestAutomaticDiscoveryClaimFailuresPropagate(t *testing.T) {
	target := "https://claim-failure.example/"
	fixture := scriptedQueue(t)
	duplicatePendingAutomaticDiscovery(t, fixture, target)
	fixture.engine.deleteErrors[orderBucket] = errors.New("delete failed")
	if _, _, err := fixture.queue.claimPendingOrder(
		t.Context(),
		"replacement",
		"worker",
		"session",
	); err == nil {
		t.Fatal("duplicate discard failure was hidden by claim")
	}
}

func TestAutomaticDiscoveryLeasePersistenceReadFailuresPropagate(t *testing.T) {
	target := "https://claim-failure.example/"
	t.Run("sidecar read", func(t *testing.T) {
		fixture := scriptedQueue(t)
		lease, record := pendingAutomaticDiscoveryLease(
			t,
			fixture,
			target,
			orderKey(7),
			false,
		)
		fixture.engine.readErrors[pendingDiscoveryKeyBucket] = errors.New("read failed")
		err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.persistPendingOrderLeaseTx(tx, lease, record)
		})
		if err == nil {
			t.Fatal("pending sidecar read failure was hidden")
		}
	})
	t.Run("sequence", func(t *testing.T) {
		fixture := scriptedQueue(t)
		lease, record := pendingAutomaticDiscoveryLease(
			t,
			fixture,
			target,
			[]byte{1},
			false,
		)
		err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.persistPendingOrderLeaseTx(tx, lease, record)
		})
		if err == nil {
			t.Fatal("malformed pending sequence was accepted")
		}
	})
	t.Run("fallback ownership read", func(t *testing.T) {
		fixture := scriptedQueue(t)
		lease, record := pendingAutomaticDiscoveryLease(
			t,
			fixture,
			target,
			orderKey(7),
			false,
		)
		fixture.engine.readErrors[activeDiscoveryKeyBucket] = errors.New("read failed")
		err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.persistPendingOrderLeaseTx(tx, lease, record)
		})
		if err == nil {
			t.Fatal("fallback ownership read failure was hidden")
		}
	})
}

func TestAutomaticDiscoveryLeasePersistenceMutationFailuresPropagate(t *testing.T) {
	target := "https://claim-failure.example/"
	t.Run("sidecar delete", func(t *testing.T) {
		fixture := scriptedQueue(t)
		lease, record := pendingAutomaticDiscoveryLease(
			t,
			fixture,
			target,
			orderKey(7),
			true,
		)
		fixture.engine.deleteErrors[pendingDiscoveryKeyBucket] = errors.New("delete failed")
		err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.persistPendingOrderLeaseTx(tx, lease, record)
		})
		if err == nil {
			t.Fatal("pending sidecar delete failure was hidden")
		}
	})
	t.Run("lease write", func(t *testing.T) {
		fixture := scriptedQueue(t)
		lease, record := pendingAutomaticDiscoveryLease(
			t,
			fixture,
			target,
			orderKey(7),
			false,
		)
		fixture.engine.putErrors[leaseBucket] = errors.New("write failed")
		err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.persistPendingOrderLeaseTx(tx, lease, record)
		})
		if err == nil {
			t.Fatal("lease write failure was hidden")
		}
	})
	t.Run("lease sidecar write", func(t *testing.T) {
		fixture := scriptedQueue(t)
		lease, record := pendingAutomaticDiscoveryLease(
			t,
			fixture,
			target,
			orderKey(7),
			true,
		)
		fixture.engine.putErrors[leasedDiscoveryKeyBucket] = errors.New("write failed")
		err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.persistPendingOrderLeaseTx(tx, lease, record)
		})
		if err == nil {
			t.Fatal("lease sidecar write failure was hidden")
		}
	})
}

func TestAutomaticDiscoveryLeaseDispositionAcknowledgmentFailuresPropagate(t *testing.T) {
	target := "https://disposition-failure.example/"
	t.Run("acknowledgment sidecar", func(t *testing.T) {
		fixture := scriptedQueue(t)
		record := leaseRecord{DiscoveryKey: target}
		seedAutomaticDiscoveryLease(t, fixture.queue, "lease", record, target)
		fixture.engine.deleteErrors[leasedDiscoveryKeyBucket] = errors.New("delete failed")
		err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.persistAcknowledgedLeaseTx(
				tx,
				"lease",
				leaseControlTarget{},
				record,
			)
		})
		if err == nil {
			t.Fatal("acknowledgment sidecar failure was hidden")
		}
	})
	t.Run("terminal acknowledgment sidecar", func(t *testing.T) {
		fixture := scriptedQueue(t)
		record := leaseRecord{DiscoveryKey: target}
		seedAutomaticDiscoveryLease(t, fixture.queue, "lease", record, target)
		fixture.engine.deleteErrors[leasedDiscoveryKeyBucket] = errors.New("delete failed")
		err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.applyTerminalLeaseDispositionTx(
				tx,
				"lease",
				terminalLeaseRequest{
					Outcome:  leaseSettlementAcknowledged,
					WorkerID: "worker",
				},
				leaseSettlementRecord{},
				record,
			)
		})
		if err == nil {
			t.Fatal("terminal acknowledgment sidecar failure was hidden")
		}
	})
}

func TestAutomaticDiscoveryLeaseDispositionRequeueFailuresPropagate(t *testing.T) {
	target := "https://disposition-failure.example/"
	t.Run("requeue ownership", func(t *testing.T) {
		fixture := scriptedQueue(t)
		record := leaseRecord{
			OrderData:    automaticDiscoveryData(t, target),
			DiscoveryKey: target,
		}
		seedAutomaticDiscoveryLease(t, fixture.queue, "lease", record, "")
		fixture.engine.putErrors[activeDiscoveryKeyBucket] = errors.New("write failed")
		err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			_, _, requeueErr := fixture.queue.requeueLeaseTx(
				tx,
				vault.Key("lease"),
				func(leaseRecord) bool { return true },
			)

			return requeueErr
		})
		if err == nil {
			t.Fatal("requeue ownership failure was hidden")
		}
	})
	t.Run("deferred requeue ownership", func(t *testing.T) {
		fixture := scriptedQueue(t)
		record := leaseRecord{
			OrderData:    automaticDiscoveryData(t, target),
			DiscoveryKey: target,
			Deferred:     true,
		}
		seedAutomaticDiscoveryLease(t, fixture.queue, "lease", record, "")
		identity := sha256.Sum256(record.OrderData)
		fixture.engine.putErrors[activeDiscoveryKeyBucket] = errors.New("write failed")
		err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			_, requeueErr := fixture.queue.requeueTerminalLeaseTx(
				tx,
				"lease",
				identity[:],
			)

			return requeueErr
		})
		if err == nil {
			t.Fatal("deferred requeue ownership failure was hidden")
		}
	})
}

func TestAutomaticDiscoveryTerminalReleaseFailuresPropagate(t *testing.T) {
	target := "https://terminal-release.example/"
	t.Run("acknowledgment", func(t *testing.T) {
		fixture := scriptedQueue(t)
		requireAutomaticDiscoveryAdmission(t, fixture.queue, target, false)
		_, leaseID, found, err := fixture.queue.leasePopForSession(
			t.Context(),
			"worker",
			"session",
		)
		if err != nil || !found {
			t.Fatalf("lease discovery = %t, %v", found, err)
		}
		fixture.engine.deleteErrors[activeDiscoveryKeyBucket] = errors.New("delete failed")
		if _, err := fixture.queue.ackLeaseWithOwner(
			t.Context(),
			leaseID,
			"worker",
			"session",
		); err == nil {
			t.Fatal("acknowledgment ownership release failure was hidden")
		}
	})
	t.Run("terminal settlement", func(t *testing.T) {
		fixture := scriptedQueue(t)
		requireAutomaticDiscoveryAdmission(t, fixture.queue, target, false)
		data, leaseID, found, err := fixture.queue.leasePopForSession(
			t.Context(),
			"worker",
			"session",
		)
		if err != nil || !found {
			t.Fatalf("lease discovery = %t, %v", found, err)
		}
		identity := sha256.Sum256(data)
		fixture.engine.deleteErrors[activeDiscoveryKeyBucket] = errors.New("delete failed")
		if _, err := fixture.queue.prepareTerminalLeaseSettlement(
			t.Context(),
			leaseID,
			terminalLeaseRequest{
				Outcome:         leaseSettlementAcknowledged,
				OrderIdentity:   identity[:],
				WorkerID:        "worker",
				WorkerSessionID: "session",
				State:           yagocrawlcontract.CrawlRunFinished,
				Tally:           yagocrawlcontract.CrawlRunTally{Failed: 1},
			},
		); err == nil {
			t.Fatal("terminal ownership release failure was hidden")
		}
	})
}
