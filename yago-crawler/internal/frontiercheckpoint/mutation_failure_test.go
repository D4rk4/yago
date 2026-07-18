package frontiercheckpoint

import (
	"errors"
	"math"
	"os"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func deleteSchemaBucket(t *testing.T, checkpoint *FrontierCheckpoint, name []byte) {
	t.Helper()
	mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
		return transaction.DeleteBucket(name)
	})
}

type missingBucketOperation struct {
	name   string
	bucket []byte
	run    func(*FrontierCheckpoint, []byte) error
}

func TestOperationsRejectMissingBuckets(t *testing.T) {
	page := testPage("https://example.com/", "example.com", "observation", 0)
	operations := append(missingBucketRunOperations(page), missingBucketProgressOperations(page)...)
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			checkpoint, provenance, _ := admittedCheckpoint(t)
			deleteSchemaBucket(t, checkpoint, operation.bucket)
			if err := operation.run(checkpoint, provenance); !errors.Is(err, ErrCorruptCheckpoint) {
				t.Fatalf("missing bucket error = %v", err)
			}
		})
	}
}

func missingBucketRunOperations(page Page) []missingBucketOperation {
	return []missingBucketOperation{
		{
			name:   "worker",
			bucket: metadataBucket,
			run: func(checkpoint *FrontierCheckpoint, _ []byte) error {
				_, err := checkpoint.WorkerID("crawler")
				return err
			},
		},
		{
			name:   "begin",
			bucket: runsBucket,
			run: func(checkpoint *FrontierCheckpoint, provenance []byte) error {
				return checkpoint.Begin(testContext, provenance, []byte("identity"), "")
			},
		},
		{
			name:   "admit",
			bucket: visitedBucket,
			run: func(checkpoint *FrontierCheckpoint, provenance []byte) error {
				_, err := checkpoint.Admit(testContext, provenance, []Page{page})
				return err
			},
		},
		{
			name:   "finish",
			bucket: pagesBucket,
			run: func(checkpoint *FrontierCheckpoint, provenance []byte) error {
				return checkpoint.FinishSeeding(testContext, provenance, testRunTally())
			},
		},
		{
			name:   "load",
			bucket: visitedBucket,
			run: func(checkpoint *FrontierCheckpoint, provenance []byte) error {
				_, err := checkpoint.Load(testContext, provenance)
				return err
			},
		},
	}
}

func missingBucketProgressOperations(page Page) []missingBucketOperation {
	return []missingBucketOperation{
		{
			name:   "complete",
			bucket: visitedBucket,
			run: func(checkpoint *FrontierCheckpoint, provenance []byte) error {
				return checkpoint.CompletePage(
					testContext,
					provenance,
					page.URL,
					testPageCompletion(),
				)
			},
		},
		{
			name:   "redirect",
			bucket: visitedBucket,
			run: func(checkpoint *FrontierCheckpoint, provenance []byte) error {
				_, err := checkpoint.RecordRedirect(
					testContext,
					provenance,
					testRedirect(page, "https://final.example/", "final.example", false),
				)
				return err
			},
		},
		{
			name:   "host",
			bucket: visitedBucket,
			run: func(checkpoint *FrontierCheckpoint, provenance []byte) error {
				return checkpoint.RecordHostState(
					testContext,
					provenance,
					page.Host,
					HostProgress{},
					nil,
				)
			},
		},
		{
			name:   "delete rows",
			bucket: visitedBucket,
			run: func(checkpoint *FrontierCheckpoint, provenance []byte) error {
				return checkpoint.Delete(testContext, provenance)
			},
		},
		{
			name:   "delete run",
			bucket: runsBucket,
			run: func(checkpoint *FrontierCheckpoint, provenance []byte) error {
				return checkpoint.Delete(testContext, provenance)
			},
		},
	}
}

func TestAdmissionRollsBackDatabaseWriteAndHostFailures(t *testing.T) {
	t.Run("oversized row", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		provenance := []byte("oversized")
		beginTestRun(t, checkpoint, provenance, []byte("identity"))
		page := testPage(strings.Repeat("u", 40_000), "example.com", "observation", 0)
		if _, err := checkpoint.Admit(testContext, provenance, []Page{page}); err == nil {
			t.Fatal("oversized row was admitted")
		}
	})
	t.Run("corrupt host", func(t *testing.T) {
		checkpoint, provenance, _ := admittedCheckpoint(t)
		prefix, _ := provenancePrefix(provenance)
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return transaction.Bucket(hostsBucket).
				Put(childRowKey(prefix, "bad.example"), []byte("{"))
		})
		page := testPage("https://bad.example/", "bad.example", "bad-observation", 1)
		if _, err := checkpoint.Admit(
			testContext,
			provenance,
			[]Page{page},
		); !errors.Is(
			err,
			ErrCorruptCheckpoint,
		) {
			t.Fatalf("corrupt host admission error = %v", err)
		}
	})
	t.Run("host overflow", func(t *testing.T) {
		checkpoint, provenance, _ := admittedCheckpoint(t)
		prefix, _ := provenancePrefix(provenance)
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return writeHostRecord(
				transaction.Bucket(
					hostsBucket,
				),
				prefix,
				"full.example",
				hostRecord{Pages: math.MaxUint64},
			)
		})
		page := testPage("https://full.example/", "full.example", "full-observation", 1)
		if _, err := checkpoint.Admit(
			testContext,
			provenance,
			[]Page{page},
		); !errors.Is(
			err,
			ErrCorruptCheckpoint,
		) {
			t.Fatalf("host overflow error = %v", err)
		}
	})
}

func TestCompletionRejectsCorruptOutstandingRows(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*bolt.Tx, []byte, Page) error
	}{
		{name: "position", mutate: func(transaction *bolt.Tx, prefix []byte, page Page) error {
			return transaction.Bucket(pagePositionsBucket).
				Put(childRowKey(prefix, page.URL), []byte{1})
		}},
		{name: "missing page", mutate: func(transaction *bolt.Tx, prefix []byte, _ Page) error {
			return transaction.Bucket(pagesBucket).Delete(sequenceRowKey(prefix, 1))
		}},
		{name: "page encoding", mutate: func(transaction *bolt.Tx, prefix []byte, _ Page) error {
			return transaction.Bucket(pagesBucket).Put(sequenceRowKey(prefix, 1), []byte("{"))
		}},
		{name: "page identity", mutate: func(transaction *bolt.Tx, prefix []byte, page Page) error {
			page.URL += "other"
			encoded, err := encodeRow("page", page)
			if err != nil {
				return err
			}
			return transaction.Bucket(pagesBucket).Put(sequenceRowKey(prefix, 1), encoded)
		}},
		{name: "pending", mutate: func(transaction *bolt.Tx, _ []byte, _ Page) error {
			record, _, err := readRunRecord(transaction, []byte("corrupt-run"))
			if err != nil {
				return err
			}
			record.Pending = 0
			return writeRunRecord(transaction, []byte("corrupt-run"), record)
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint, provenance, page := admittedCheckpoint(t)
			prefix, _ := provenancePrefix(provenance)
			mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
				return testCase.mutate(transaction, prefix, page)
			})
			if err := checkpoint.CompletePage(
				testContext,
				provenance,
				page.URL,
				testPageCompletion(),
			); !errors.Is(
				err,
				ErrCorruptCheckpoint,
			) {
				t.Fatalf("completion error = %v", err)
			}
		})
	}
}

func TestHostStateRejectsMismatchedAndExcessDroppedPages(t *testing.T) {
	t.Run("corrupt state", func(t *testing.T) {
		checkpoint, provenance, page := admittedCheckpoint(t)
		prefix, _ := provenancePrefix(provenance)
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return transaction.Bucket(hostsBucket).Put(childRowKey(prefix, page.Host), []byte("{"))
		})
		if err := checkpoint.RecordHostState(
			testContext, provenance, page.Host, HostProgress{Failures: 1, Retired: true}, nil,
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("corrupt host state error = %v", err)
		}
	})
	t.Run("host", func(t *testing.T) {
		checkpoint, provenance, page := admittedCheckpoint(t)
		if err := checkpoint.RecordHostState(
			testContext,
			provenance,
			"other.example",
			HostProgress{Failures: 1, Retired: true},
			[]string{page.URL},
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("host mismatch error = %v", err)
		}
	})
	t.Run("pending", func(t *testing.T) {
		checkpoint, provenance, page := admittedCheckpoint(t)
		mutateRunRecord(t, checkpoint, provenance, func(record *runRecord) { record.Pending = 0 })
		if err := checkpoint.RecordHostState(
			testContext,
			provenance,
			page.Host,
			HostProgress{Failures: 1, Retired: true},
			[]string{page.URL},
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("excess dropped page error = %v", err)
		}
	})
}

func TestWriteRunRejectsMissingRunBucket(t *testing.T) {
	checkpoint, provenance, _ := admittedCheckpoint(t)
	err := checkpoint.writeTransaction(testContext, func(transaction *bolt.Tx) error {
		if err := transaction.DeleteBucket(runsBucket); err != nil {
			return wrapDatabaseError("delete run bucket", err)
		}
		return writeRunRecord(transaction, provenance, runRecord{OrderIdentity: []byte("identity")})
	})
	if !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("missing run bucket write error = %v", err)
	}
}

func TestRedirectPropagatesVisitedAndHostWriteFailures(t *testing.T) {
	t.Run("visited", func(t *testing.T) {
		checkpoint, provenance, source := admittedCheckpoint(t)
		finalURL := strings.Repeat("r", 40_000)
		if _, err := checkpoint.RecordRedirect(
			testContext,
			provenance,
			testRedirect(
				source,
				finalURL,
				"example.com",
				false,
			),
		); err == nil {
			t.Fatal("oversized redirect was recorded")
		}
		assertRedirectRolledBack(t, checkpoint, provenance, finalURL)
	})
	t.Run("host", func(t *testing.T) {
		checkpoint, provenance, source := admittedCheckpoint(t)
		prefix, _ := provenancePrefix(provenance)
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return transaction.Bucket(hostsBucket).
				Put(childRowKey(prefix, "bad.example"), []byte("{"))
		})
		if _, err := checkpoint.RecordRedirect(
			testContext,
			provenance,
			testRedirect(
				source,
				"https://bad.example/",
				"bad.example",
				true,
			),
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("corrupt redirect host error = %v", err)
		}
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return deleteRow(
				transaction.Bucket(hostsBucket),
				childRowKey(prefix, "bad.example"),
				"corrupt redirect host",
			)
		})
		assertRedirectRolledBack(t, checkpoint, provenance, "https://bad.example/")
	})
}

func assertRedirectRolledBack(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	provenance []byte,
	finalURL string,
) {
	t.Helper()
	snapshot, err := checkpoint.Load(testContext, provenance)
	if err != nil {
		t.Fatalf("load rolled-back redirect: %v", err)
	}
	if len(snapshot.Outstanding) != 1 || snapshot.Outstanding[0].RedirectURL != "" {
		t.Fatalf("redirect page mutation committed: %+v", snapshot.Outstanding)
	}
	if _, found := snapshot.Visited[finalURL]; found {
		t.Fatalf("redirect target %q remained reserved", finalURL)
	}
}

func TestDirectReadOnlyDeletionReportsDatabaseError(t *testing.T) {
	checkpoint, provenance, _ := admittedCheckpoint(t)
	prefix, _ := provenancePrefix(provenance)
	err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
		_, err := deletePrefixedRows(
			transaction.Bucket(visitedBucket),
			prefix,
			deletionRowsPerTransaction,
		)
		return err
	})
	if err == nil {
		t.Fatal("read-only deletion succeeded")
	}
}

func TestLifecycleAndSchemaCreationFailures(t *testing.T) {
	checkpoint := &FrontierCheckpoint{}
	if err := checkpoint.initialize("/path/that/does/not/exist"); err == nil {
		t.Fatal("initialize accepted missing database path")
	}
	if _, err := Open("/proc/self/frontier-checkpoint.db"); err == nil {
		t.Fatal("open unexpectedly secured procfs directory")
	}
	path := testCheckpointPath(t)
	original := allSchemaBuckets
	allSchemaBuckets = [][]byte{metadataBucket, metadataBucket}
	t.Cleanup(func() { allSchemaBuckets = original })
	if _, err := Open(path); err == nil {
		t.Fatal("duplicate schema buckets were created")
	}
	if _, err := os.Stat(path); err != nil && !os.IsNotExist(err) {
		t.Fatalf("stat failed schema database: %v", err)
	}
}
