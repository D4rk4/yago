package frontiercheckpoint

import (
	"errors"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestBoundedRecoveryRejectsMissingFrontierBuckets(t *testing.T) {
	for _, operation := range []struct {
		name string
		run  func(*FrontierCheckpoint, []byte) error
	}{
		{name: "load", run: func(checkpoint *FrontierCheckpoint, provenance []byte) error {
			_, err := checkpoint.LoadBounded(testContext, provenance, 1)
			return err
		}},
		{name: "batch", run: func(checkpoint *FrontierCheckpoint, provenance []byte) error {
			_, err := checkpoint.LoadRecoveryPageBatch(testContext, provenance, 0, 1, 1)
			return err
		}},
		{name: "cancel", run: func(checkpoint *FrontierCheckpoint, provenance []byte) error {
			_, err := checkpoint.CancelRecoveryPages(testContext, provenance, 0, 1)
			return err
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			checkpoint, provenance, _ := admittedCheckpoint(t)
			if operation.name == "cancel" {
				if err := checkpoint.UpdateControl(
					testContext,
					provenance,
					ControlUpdate{Cancelled: true},
				); err != nil {
					t.Fatalf("mark cancellation fixture: %v", err)
				}
			}
			deleteSchemaBucket(t, checkpoint, visitedBucket)
			if err := operation.run(checkpoint, provenance); !errors.Is(err, ErrCorruptCheckpoint) {
				t.Fatalf("missing bounded bucket error = %v", err)
			}
		})
	}
}

func TestCancelRecoveryRejectsMissingRunAndPendingUnderflow(t *testing.T) {
	t.Run("missing run", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		if _, err := checkpoint.CancelRecoveryPages(
			testContext, []byte("missing"), 0, 1,
		); !errors.Is(err, ErrRunNotFound) {
			t.Fatalf("missing cancellation run error = %v", err)
		}
	})
	t.Run("pending", func(t *testing.T) {
		checkpoint, provenance, _ := admittedCheckpoint(t)
		mutateRunRecord(t, checkpoint, provenance, func(record *runRecord) {
			record.Cancelled = true
			record.Pending = 0
		})
		if _, err := checkpoint.CancelRecoveryPages(
			testContext, provenance, 0, 1,
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("cancellation pending underflow error = %v", err)
		}
	})
}

func TestRetiredRecoveryRejectsRemovalFailureAndPendingUnderflow(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*bolt.Tx, []byte, Page, *runRecord) error
	}{
		{name: "position", mutate: func(transaction *bolt.Tx, prefix []byte, page Page, record *runRecord) error {
			host, err := readHostRecord(transaction.Bucket(hostsBucket), prefix, page.Host)
			if err != nil {
				return err
			}
			host.Retired = true
			if err := writeHostRecord(transaction.Bucket(hostsBucket), prefix, page.Host, host); err != nil {
				return err
			}
			return transaction.Bucket(pagePositionsBucket).Put(childRowKey(prefix, page.URL), []byte{1})
		}},
		{name: "pending", mutate: func(transaction *bolt.Tx, prefix []byte, page Page, record *runRecord) error {
			host, err := readHostRecord(transaction.Bucket(hostsBucket), prefix, page.Host)
			if err != nil {
				return err
			}
			host.Retired = true
			record.Pending = 0
			return writeHostRecord(transaction.Bucket(hostsBucket), prefix, page.Host, host)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint, provenance, page := admittedCheckpoint(t)
			prefix, _ := provenancePrefix(provenance)
			mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
				record, _, err := readRunRecord(transaction, provenance)
				if err != nil {
					return err
				}
				if err := testCase.mutate(transaction, prefix, page, &record); err != nil {
					return err
				}
				return writeRunRecord(transaction, provenance, record)
			})
			if _, err := checkpoint.LoadBounded(
				testContext,
				provenance,
				1,
			); !errors.Is(
				err,
				ErrCorruptCheckpoint,
			) {
				t.Fatalf("retired recovery error = %v", err)
			}
		})
	}
}

func TestRecoveryPageURLScannerRejectsCorruptRowsAndHonorsUpperBoundary(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*bolt.Tx, []byte, Page) error
	}{
		{name: "encoding", mutate: func(transaction *bolt.Tx, prefix []byte, _ Page) error {
			return transaction.Bucket(pagesBucket).Put(sequenceRowKey(prefix, 1), []byte("{"))
		}},
		{name: "page", mutate: func(transaction *bolt.Tx, prefix []byte, page Page) error {
			page.Host = ""
			encoded, _ := encodeRow("page", page)
			return transaction.Bucket(pagesBucket).Put(sequenceRowKey(prefix, 1), encoded)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint, provenance, page := admittedCheckpoint(t)
			prefix, _ := provenancePrefix(provenance)
			mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
				return testCase.mutate(transaction, prefix, page)
			})
			if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
				_, _, _, err := recoveryPageURLs(transaction.Bucket(pagesBucket), prefix, 0, 1, 1)
				return err
			}); !errors.Is(err, ErrCorruptCheckpoint) {
				t.Fatalf("recovery URL scanner error = %v", err)
			}
		})
	}
	t.Run("upper boundary", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		provenance := []byte("recovery-upper")
		beginTestRun(t, checkpoint, provenance, []byte("identity"))
		first := testPage("https://upper.example/one", "upper.example", "one", 0)
		second := testPage("https://upper.example/two", "upper.example", "two", 1)
		if _, err := checkpoint.Admit(testContext, provenance, []Page{first, second}); err != nil {
			t.Fatalf("admit upper boundary pages: %v", err)
		}
		prefix, _ := provenancePrefix(provenance)
		if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
			urls, cursor, complete, err := recoveryPageURLs(
				transaction.Bucket(pagesBucket), prefix, 0, 1, 2,
			)
			if err != nil || len(urls) != 1 || cursor != 1 || !complete {
				t.Fatalf("upper bounded URLs = %v, %d, %v, %v", urls, cursor, complete, err)
			}
			return nil
		}); err != nil {
			t.Fatalf("inspect recovery upper boundary: %v", err)
		}
	})
}

func TestRecoveryRedirectHostCorruptionIsRejected(t *testing.T) {
	checkpoint, provenance, page := admittedCheckpoint(t)
	if recorded, err := checkpoint.RecordRedirect(testContext, provenance, Redirect{
		SourceURL:     page.URL,
		FinalURL:      "https://redirect.example/page",
		FinalHost:     "redirect.example",
		IncrementHost: true,
	}); err != nil || !recorded {
		t.Fatalf("record recovery redirect = %v, %v", recorded, err)
	}
	prefix, _ := provenancePrefix(provenance)
	mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
		return transaction.Bucket(hostsBucket).
			Put(childRowKey(prefix, "redirect.example"), []byte("{"))
	})
	if _, err := checkpoint.LoadBounded(
		testContext,
		provenance,
		1,
	); !errors.Is(
		err,
		ErrCorruptCheckpoint,
	) {
		t.Fatalf("corrupt redirect host recovery error = %v", err)
	}
}
