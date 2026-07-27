package remotecrawl

import (
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func remoteCrawlOrderAt(
	t *testing.T,
	storage *vault.Vault,
	broker *Broker,
	sequence uint64,
) (queueRecord, bool) {
	t.Helper()
	var record queueRecord
	var found bool
	if err := storage.View(t.Context(), func(tx *vault.Txn) error {
		var err error
		record, found, err = broker.orders.Get(tx, sequenceKey(sequence))
		if err != nil {
			return fmt.Errorf("read remote crawl order under test: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	return record, found
}

func TestRemoteCrawlRejectedOrderDeletionRequiresTheSameOrder(t *testing.T) {
	now := time.Unix(100, 0)
	broker, storage := openMemoryBroker(
		t,
		remoteConfig(func() time.Time { return now }),
		&recordingReceiver{},
	)
	stageURL(t, broker, testURLA)
	hashA, err := yagomodel.HashURL(testURLA)
	if err != nil {
		t.Fatal(err)
	}
	hashB, err := yagomodel.HashURL(testURLB)
	if err != nil {
		t.Fatal(err)
	}
	if err := broker.deletePendingOrder(t.Context(), queueRecord{
		Sequence: 0, URLHash: hashB.String(), State: queueStatePending,
	}); err != nil {
		t.Fatal(err)
	}
	record, found := remoteCrawlOrderAt(t, storage, broker, 0)
	if !found || record.URLHash != hashA.String() {
		t.Fatalf("order removed by a rejection naming another URL = %+v, %t", record, found)
	}
	if _, err := broker.URLsForRemoteCrawl(t.Context(), testPeerA, 1, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := broker.deletePendingOrder(t.Context(), queueRecord{
		Sequence: 0, URLHash: hashA.String(), State: queueStatePending,
	}); err != nil {
		t.Fatal(err)
	}
	record, found = remoteCrawlOrderAt(t, storage, broker, 0)
	if !found || record.State != queueStateLeased || record.Peer != testPeerA.String() {
		t.Fatalf("leased order removed by a pending rejection = %+v, %t", record, found)
	}
}

func TestRemoteCrawlLeaseRefusesAStoredDestinationThatIsNotCanonical(t *testing.T) {
	broker, storage := openMemoryBroker(t, remoteConfig(time.Now), &recordingReceiver{})
	uncanonical := testURLA + "#fragment"
	hash, err := yagomodel.HashURL(uncanonical)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		if err := broker.orders.Put(tx, sequenceKey(0), queueRecord{
			Sequence: 0, URL: uncanonical, URLHash: hash.String(), State: queueStatePending,
		}); err != nil {
			return fmt.Errorf("store uncanonical remote crawl order: %w", err)
		}
		if err := broker.pending.Put(
			tx,
			sequenceKey(0),
			pendingRecord{Sequence: 0},
		); err != nil {
			return fmt.Errorf("index uncanonical remote crawl order: %w", err)
		}
		if err := broker.urlSequences.Put(tx, vault.Key(hash.String()), 0); err != nil {
			return fmt.Errorf("store uncanonical remote crawl URL sequence: %w", err)
		}
		if err := broker.sequence.Put(tx, nextSequenceKey, 1); err != nil {
			return fmt.Errorf("advance uncanonical remote crawl sequence: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	leased, err := broker.URLsForRemoteCrawl(t.Context(), testPeerA, 1, time.Second)
	if err != nil || len(leased) != 0 {
		t.Fatalf("uncanonical lease = %+v, %v", leased, err)
	}
	if count, err := broker.PendingCount(t.Context()); err != nil || count != 0 {
		t.Fatalf("pending work after uncanonical rejection = %d, %v", count, err)
	}
}
