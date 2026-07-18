package frontiercheckpoint

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestBoundedWriteTransactionRejectsCancelledAndClosedCheckpoints(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	cancelled, cancel := context.WithCancel(testContext)
	cancel()
	if err := checkpoint.boundedWriteTransaction(cancelled, func(*bolt.Tx) error {
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled bounded write error = %v", err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close bounded write fixture: %v", err)
	}
	if err := checkpoint.boundedWriteTransaction(testContext, func(*bolt.Tx) error {
		return nil
	}); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed bounded write error = %v", err)
	}
}

func TestBoundedRecoveryEntryPointsPropagatePersistedFaults(t *testing.T) {
	t.Run("batch page", func(t *testing.T) {
		checkpoint, provenance, _ := admittedCheckpoint(t)
		prefix, _ := provenancePrefix(provenance)
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return transaction.Bucket(pagesBucket).Put(sequenceRowKey(prefix, 1), []byte("{"))
		})
		if _, err := checkpoint.LoadRecoveryPageBatch(
			testContext, provenance, 0, 1, 1,
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("corrupt recovery batch error = %v", err)
		}
	})
	t.Run("cancellation page", func(t *testing.T) {
		checkpoint, provenance, _ := admittedCheckpoint(t)
		prefix, _ := provenancePrefix(provenance)
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			record, _, err := readRunRecord(transaction, provenance)
			if err != nil {
				return err
			}
			record.Cancelled = true
			if err := writeRunRecord(transaction, provenance, record); err != nil {
				return err
			}
			return transaction.Bucket(pagesBucket).Put(sequenceRowKey(prefix, 1), []byte("{"))
		})
		if _, err := checkpoint.CancelRecoveryPages(
			testContext, provenance, 0, 1,
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("corrupt cancellation recovery error = %v", err)
		}
	})
	t.Run("bounded manifest bucket", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		provenance := []byte("bounded-manifest-bucket")
		beginSeedManifest(t, checkpoint, provenance, []Page{
			testPage("https://seed.example/page", "seed.example", "seed", 0),
		})
		deleteSchemaBucket(t, checkpoint, seedManifestBucket)
		if _, err := checkpoint.LoadBounded(
			testContext, provenance, 1,
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("missing bounded manifest error = %v", err)
		}
	})
	t.Run("retired page storage", func(t *testing.T) {
		checkpoint, provenance, page := admittedCheckpoint(t)
		prefix, _ := provenancePrefix(provenance)
		if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
			buckets, err := loadCheckpointBuckets(transaction)
			if err != nil {
				return err
			}
			record, _, err := readRunRecord(transaction, provenance)
			if err != nil {
				return err
			}
			return dropRetiredRecoveryPages(
				buckets,
				prefix,
				[]string{page.URL},
				&record,
				&RecoveryPageBatch{},
			)
		}); err == nil {
			t.Fatal("read-only retired page removal succeeded")
		}
	})
}

func TestCancelSeedManifestRequiresFrontierBeforeFinalSettlement(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("cancel-seed-missing-pages")
	beginSeedManifest(t, checkpoint, provenance, []Page{
		testPage("https://seed.example/page", "seed.example", "seed", 0),
	})
	mutateRunRecord(t, checkpoint, provenance, func(record *runRecord) {
		record.Cancelled = true
	})
	deleteSchemaBucket(t, checkpoint, pagesBucket)
	if done, err := checkpoint.CancelSeedManifestBatch(
		testContext, provenance,
	); done || !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("cancel seed without pages bucket = %v, %v", done, err)
	}
}

func TestHostPaceInternalTransitionsPropagateStorageFailures(t *testing.T) {
	testHostPaceReadAndSchemaFailures(t)
	testHostPaceLedgerMutationFailures(t)
}

func testHostPaceReadAndSchemaFailures(t *testing.T) {
	t.Helper()
	t.Run("load metadata write", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
			_, err := loadHostPaces(transaction, 1)
			return err
		}); err == nil {
			t.Fatal("read-only pace load metadata write succeeded")
		}
	})
	t.Run("collect corrupt pace", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return transaction.Bucket(hostPacesBucket).Put([]byte("pace.example"), []byte("{"))
		})
		if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
			_, err := collectHostPaces(transaction.Bucket(hostPacesBucket))
			return err
		}); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("direct corrupt pace collection error = %v", err)
		}
	})
	t.Run("invalid record", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		if err := checkpoint.writeTransaction(testContext, func(transaction *bolt.Tx) error {
			return recordHostPace(transaction, " ", validPaceState(1), 1)
		}); !errors.Is(err, ErrInvalidHostState) {
			t.Fatalf("invalid direct pace error = %v", err)
		}
	})
	for _, bucket := range [][]byte{metadataBucket, hostPacesBucket, hostPaceOrderBucket} {
		t.Run("missing "+string(bucket), func(t *testing.T) {
			checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
			deleteSchemaBucket(t, checkpoint, bucket)
			err := checkpoint.writeTransaction(testContext, func(transaction *bolt.Tx) error {
				return recordHostPace(transaction, "pace.example", validPaceState(1), 1)
			})
			if !errors.Is(err, ErrCorruptCheckpoint) {
				t.Fatalf("missing pace bucket error = %v", err)
			}
		})
	}
}

func testHostPaceLedgerMutationFailures(t *testing.T) {
	t.Helper()
	t.Run("trim after append", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		if err := checkpoint.writeTransaction(testContext, func(transaction *bolt.Tx) error {
			return recordHostPace(transaction, "first.example", validPaceState(1), 2)
		}); err != nil {
			t.Fatalf("record first pace: %v", err)
		}
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return transaction.Bucket(hostPacesBucket).Delete([]byte("first.example"))
		})
		err := checkpoint.writeTransaction(testContext, func(transaction *bolt.Tx) error {
			return recordHostPace(transaction, "second.example", validPaceState(1), 1)
		})
		if !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("pace trim after append error = %v", err)
		}
	})
	t.Run("replace order in read-only transaction", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		if err := checkpoint.writeTransaction(testContext, func(transaction *bolt.Tx) error {
			return recordHostPace(transaction, "pace.example", validPaceState(1), 1)
		}); err != nil {
			t.Fatalf("record pace fixture: %v", err)
		}
		if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
			paces := transaction.Bucket(hostPacesBucket)
			order := transaction.Bucket(hostPaceOrderBucket)
			_, _, err := prepareHostPaceRecord(
				paces, order, []byte("pace.example"), validPaceState(2), 1,
			)
			return err
		}); err == nil {
			t.Fatal("read-only pace order replacement succeeded")
		}
	})
}

func TestRedirectReplacementPropagatesEveryAtomicWriteFailure(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*bolt.Tx, []byte, string) error
		target string
		host   string
	}{
		{
			name: "visited marker",
			mutate: func(transaction *bolt.Tx, prefix []byte, target string) error {
				return transaction.Bucket(visitedBucket).Put(childRowKey(prefix, target), []byte{2})
			},
			target: "https://second.example/page",
			host:   "second.example",
		},
		{
			name:   "visited key size",
			mutate: func(*bolt.Tx, []byte, string) error { return nil },
			target: "https://second.example/" + strings.Repeat("x", bolt.MaxKeySize),
			host:   "second.example",
		},
		{
			name: "host state",
			mutate: func(transaction *bolt.Tx, prefix []byte, _ string) error {
				return transaction.Bucket(hostsBucket).Put(childRowKey(prefix, "second.example"), []byte("{"))
			},
			target: "https://second.example/page",
			host:   "second.example",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint, provenance, page := admittedCheckpoint(t)
			first := Redirect{
				SourceURL:     page.URL,
				FinalURL:      "https://first.example/page",
				FinalHost:     "first.example",
				IncrementHost: true,
			}
			if admitted, err := checkpoint.RecordRedirect(
				testContext, provenance, first,
			); err != nil || !admitted {
				t.Fatalf("record first redirect = %v, %v", admitted, err)
			}
			prefix, _ := provenancePrefix(provenance)
			mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
				return testCase.mutate(transaction, prefix, testCase.target)
			})
			_, err := checkpoint.RecordRedirect(testContext, provenance, Redirect{
				SourceURL:     page.URL,
				FinalURL:      testCase.target,
				FinalHost:     testCase.host,
				IncrementHost: true,
			})
			if err == nil {
				t.Fatal("faulted redirect replacement succeeded")
			}
		})
	}
}

func TestRedirectedPageEncodingFailureRollsBackReleasedReservation(t *testing.T) {
	checkpoint, provenance, page := admittedCheckpoint(t)
	first := Redirect{
		SourceURL:     page.URL,
		FinalURL:      "https://first.example/page",
		FinalHost:     "first.example",
		IncrementHost: true,
	}
	if admitted, err := checkpoint.RecordRedirect(
		testContext, provenance, first,
	); err != nil || !admitted {
		t.Fatalf("record first redirect = %v, %v", admitted, err)
	}
	prefix, _ := provenancePrefix(provenance)
	err := checkpoint.writeTransaction(testContext, func(transaction *bolt.Tx) error {
		buckets, loadErr := loadCheckpointBuckets(transaction)
		if loadErr != nil {
			return loadErr
		}
		row, found, findErr := findOutstandingPage(buckets, prefix, page.URL)
		if findErr != nil || !found {
			return findErr
		}
		row.page.ObservedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
		admitted := false
		return updateRedirectReservation(buckets, prefix, row, Redirect{
			SourceURL:     page.URL,
			FinalURL:      "https://second.example/page",
			FinalHost:     "second.example",
			IncrementHost: true,
		}, &admitted)
	})
	if err == nil {
		t.Fatal("unencodable redirect replacement succeeded")
	}
	current, err := checkpoint.Load(testContext, provenance)
	if err != nil || len(current.Outstanding) != 1 ||
		current.Outstanding[0].RedirectURL != first.FinalURL {
		t.Fatalf("redirect encoding rollback = %+v, %v", current.Outstanding, err)
	}
}

func TestDirectFrontierMutationHelpersRejectCorruptAndReadOnlyState(t *testing.T) {
	t.Run("decrement corrupt host", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return transaction.Bucket(hostsBucket).Put([]byte("host"), []byte("{"))
		})
		if err := checkpoint.writeTransaction(testContext, func(transaction *bolt.Tx) error {
			return decrementHostPages(transaction.Bucket(hostsBucket), nil, "host")
		}); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("corrupt host decrement error = %v", err)
		}
	})
	t.Run("corrupt cancellation run", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		provenance := []byte("corrupt-cancel-transition")
		prefix, _ := provenancePrefix(provenance)
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return transaction.Bucket(runsBucket).Put(provenance, []byte("{"))
		})
		if err := checkpoint.writeTransaction(testContext, func(transaction *bolt.Tx) error {
			_, err := resumeCancelledRunTransition(transaction, provenance, prefix)
			return err
		}); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("corrupt cancellation transition error = %v", err)
		}
	})
	t.Run("missing manifest leaf", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		deleteSchemaBucket(t, checkpoint, seedManifestBucket)
		if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
			_, err := runLeafBuckets(transaction)
			return err
		}); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("missing run leaf manifest error = %v", err)
		}
	})
	t.Run("missing snapshot manifest", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		deleteSchemaBucket(t, checkpoint, seedManifestBucket)
		if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
			return loadSeedManifest(transaction, []byte{1}, runRecord{}, &Snapshot{})
		}); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("missing snapshot manifest error = %v", err)
		}
	})
}
