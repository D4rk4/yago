package frontiercheckpoint

import (
	"errors"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlpace"
)

func TestReadOnlyTransactionRejectsFrontierMutations(t *testing.T) {
	checkpoint, provenance, page := admittedCheckpoint(t)
	prefix, _ := provenancePrefix(provenance)
	if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
		buckets, err := loadCheckpointBuckets(transaction)
		if err != nil {
			return err
		}
		if _, err := removeOutstandingPage(buckets, prefix, page.URL, ""); err == nil {
			t.Fatal("read-only page removal succeeded")
		}
		if _, err := completeOutstandingPage(
			transaction, provenance, prefix, page.URL, PageCompletion{},
		); err == nil {
			t.Fatal("read-only page completion succeeded")
		}
		if _, err := removeOutstandingPage(
			buckets,
			prefix,
			page.URL,
			"other.example",
		); !errors.Is(
			err,
			ErrCorruptCheckpoint,
		) {
			t.Fatalf("required host mismatch error = %v", err)
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect read-only frontier mutation: %v", err)
	}
}

func TestReadOnlyTransactionRejectsRedirectMutationStages(t *testing.T) {
	checkpoint, provenance, page := admittedCheckpoint(t)
	target := "https://target.example/page"
	if recorded, err := checkpoint.RecordRedirect(testContext, provenance, Redirect{
		SourceURL: page.URL, FinalURL: target, FinalHost: "target.example", IncrementHost: true,
	}); err != nil || !recorded {
		t.Fatalf("record redirect fixture = %v, %v", recorded, err)
	}
	prefix, _ := provenancePrefix(provenance)
	if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
		buckets, err := loadCheckpointBuckets(transaction)
		if err != nil {
			return err
		}
		row, found, err := findOutstandingPage(buckets, prefix, page.URL)
		if err != nil || !found {
			return err
		}
		admitted := false
		if err := updateRedirectReservation(
			buckets,
			prefix,
			row,
			Redirect{SourceURL: page.URL},
			&admitted,
		); err == nil {
			t.Fatal("read-only redirect release succeeded")
		}
		row.page.RedirectURL = "https://replacement.example/page"
		row.page.RedirectHost = "replacement.example"
		row.page.RedirectHostBump = true
		if err := writeRedirectedPage(buckets.pages, row); err == nil {
			t.Fatal("read-only redirected page write succeeded")
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect read-only redirect mutation: %v", err)
	}
}

func TestReadOnlyTransactionRejectsPaceAndTerminalWrites(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	settlement := completedTerminalSettlement(t, checkpoint, "read-only-write")
	if err := checkpoint.Stage(testContext, settlement); err != nil {
		t.Fatalf("stage terminal write fixture: %v", err)
	}
	if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
		if err := recordHostPace(transaction, "pace.example", validPaceState(1), 1); err == nil {
			t.Fatal("read-only host pace write succeeded")
		}
		outbox := transaction.Bucket(terminalOutboxBucket)
		if err := writeTerminalSettlement(outbox, settlement); err == nil {
			t.Fatal("read-only terminal settlement write succeeded")
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect read-only durable writes: %v", err)
	}
}

func TestHostPaceEncodingFailureRollsBackSequence(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	invalidTime := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	state := crawlpace.HostState{NextDueAt: invalidTime, Generation: 1}
	err := checkpoint.writeTransaction(testContext, func(transaction *bolt.Tx) error {
		return recordHostPace(transaction, "pace.example", state, 1)
	})
	if err == nil {
		t.Fatal("unencodable host pace succeeded")
	}
	states, err := checkpoint.HostPaces(testContext, 1)
	if err != nil || len(states) != 0 {
		t.Fatalf("host pace encoding rollback = %+v, %v", states, err)
	}
}

func TestHostPaceCollectionRejectsPersistedCorruptRecords(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		record []byte
	}{
		{name: "encoding", record: []byte("{")},
		{name: "state", record: func() []byte {
			encoded, _ := encodeRow("pace", hostPaceRecord{})
			return encoded
		}()},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
			mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
				host := []byte("pace.example")
				if err := transaction.Bucket(hostPacesBucket).
					Put(host, testCase.record); err != nil {
					return wrapDatabaseError("write corrupt pace collection fixture", err)
				}
				if err := transaction.Bucket(hostPaceOrderBucket).
					Put(sequenceValue(1), host); err != nil {
					return wrapDatabaseError("write corrupt pace order fixture", err)
				}
				return transaction.Bucket(metadataBucket).Put(hostPaceTotalKey, sequenceValue(1))
			})
			if _, err := checkpoint.HostPaces(
				testContext,
				1,
			); !errors.Is(
				err,
				ErrCorruptCheckpoint,
			) {
				t.Fatalf("corrupt pace collection error = %v", err)
			}
		})
	}
}
