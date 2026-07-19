package remotecrawl

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func writeQueueStateUpgradeFixture(
	t *testing.T,
	path string,
	now time.Time,
) (int64, collections) {
	t.Helper()
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	collections, err := registerCollections(storage)
	if err != nil {
		t.Fatal(err)
	}
	hashA, err := yagomodel.HashURL(testURLA)
	if err != nil {
		t.Fatal(err)
	}
	hashB, err := yagomodel.HashURL(testURLB)
	if err != nil {
		t.Fatal(err)
	}
	leaseUntil := now.Add(time.Minute).UnixNano()
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return storeQueueStateUpgradeFixture(tx, collections, hashA, hashB, leaseUntil)
	}); err != nil {
		t.Fatal(err)
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}

	return leaseUntil, collections
}

func storeQueueStateUpgradeFixture(
	tx *vault.Txn,
	collections collections,
	hashA yagomodel.URLHash,
	hashB yagomodel.URLHash,
	leaseUntil int64,
) error {
	pending := queueRecord{
		Sequence: 0, URL: testURLA, URLHash: hashA.String(), State: queueStatePending,
	}
	leased := queueRecord{
		Sequence: 1, URL: testURLB, URLHash: hashB.String(),
		State: queueStateLeased, Peer: testPeerA.String(), LeaseUntil: leaseUntil,
	}
	if err := collections.orders.Put(tx, sequenceKey(0), pending); err != nil {
		return fmt.Errorf("store pending upgrade fixture: %w", err)
	}
	if err := collections.orders.Put(tx, sequenceKey(1), leased); err != nil {
		return fmt.Errorf("store leased upgrade fixture: %w", err)
	}
	if err := collections.urlSequences.Put(tx, vault.Key(hashA.String()), 0); err != nil {
		return fmt.Errorf("store pending fixture URL sequence: %w", err)
	}
	if err := collections.urlSequences.Put(tx, vault.Key(hashB.String()), 1); err != nil {
		return fmt.Errorf("store leased fixture URL sequence: %w", err)
	}
	if err := collections.sequence.Put(tx, nextSequenceKey, 2); err != nil {
		return fmt.Errorf("store fixture next sequence: %w", err)
	}
	if err := collections.pending.Put(
		tx,
		sequenceKey(9),
		pendingRecord{Sequence: 9},
	); err != nil {
		return fmt.Errorf("store stale pending fixture: %w", err)
	}
	if err := collections.leaseExpiries.Put(
		tx,
		leaseExpiryKey(leaseUntil+1, 9),
		leaseExpiryRecord{Sequence: 9},
	); err != nil {
		return fmt.Errorf("store stale lease expiry fixture: %w", err)
	}
	if err := collections.leaseCounts.Put(tx, vault.Key(testPeerA.String()), 9); err != nil {
		return fmt.Errorf("store stale lease count fixture: %w", err)
	}

	return nil
}

func assertQueueStateUpgrade(
	t *testing.T,
	storage *vault.Vault,
	collections collections,
	leaseUntil int64,
) {
	t.Helper()
	if err := storage.View(t.Context(), func(tx *vault.Txn) error {
		if collections.pending.Contains(tx, sequenceKey(9)) {
			return errors.New("stale pending key survived reconciliation")
		}
		if collections.leaseExpiries.Contains(tx, leaseExpiryKey(leaseUntil+1, 9)) {
			return errors.New("stale lease expiry survived reconciliation")
		}
		version, found, err := collections.schema.Get(tx, queueStateVersionKey)
		if err != nil {
			return fmt.Errorf("read reconciled queue state version: %w", err)
		}
		if !found || version != currentQueueStateVersion {
			return fmt.Errorf("queue state version = %d, %v", version, found)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
