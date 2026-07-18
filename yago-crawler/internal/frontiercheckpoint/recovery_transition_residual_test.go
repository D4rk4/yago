package frontiercheckpoint

import (
	"errors"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestHostPageValidationRejectsCorruptOutstandingLookup(t *testing.T) {
	checkpoint, provenance, page := admittedCheckpoint(t)
	prefix, _ := provenancePrefix(provenance)
	mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
		return transaction.Bucket(pagePositionsBucket).Put(childRowKey(prefix, page.URL), []byte{1})
	})
	if err := checkpoint.validateHostPages(
		testContext, provenance, prefix, page.Host, []string{page.URL},
	); !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("corrupt host-page lookup error = %v", err)
	}
}

type hostPageRemovalFault struct {
	name   string
	mutate func(*bolt.Tx, []byte, []byte, Page, *runRecord) error
}

var hostPageRemovalFaults = []hostPageRemovalFault{
	{
		name: "missing run",
		mutate: func(transaction *bolt.Tx, provenance, _ []byte, _ Page, _ *runRecord) error {
			return transaction.Bucket(runsBucket).Delete(provenance)
		},
	},
	{
		name: "missing bucket",
		mutate: func(transaction *bolt.Tx, _, _ []byte, _ Page, _ *runRecord) error {
			return transaction.DeleteBucket(visitedBucket)
		},
	},
	{
		name: "host encoding",
		mutate: func(transaction *bolt.Tx, _, prefix []byte, page Page, _ *runRecord) error {
			return transaction.Bucket(hostsBucket).Put(childRowKey(prefix, page.Host), []byte("{"))
		},
	},
	{
		name: "page removal",
		mutate: func(transaction *bolt.Tx, _, prefix []byte, page Page, _ *runRecord) error {
			host, err := readHostRecord(transaction.Bucket(hostsBucket), prefix, page.Host)
			if err != nil {
				return err
			}
			host.Retired = true
			host.Generation = 1
			if err := writeHostRecord(
				transaction.Bucket(hostsBucket),
				prefix,
				page.Host,
				host,
			); err != nil {
				return err
			}
			return transaction.Bucket(pagePositionsBucket).
				Put(childRowKey(prefix, page.URL), []byte{1})
		},
	},
	{
		name: "pending",
		mutate: func(transaction *bolt.Tx, _, prefix []byte, page Page, record *runRecord) error {
			host, err := readHostRecord(transaction.Bucket(hostsBucket), prefix, page.Host)
			if err != nil {
				return err
			}
			host.Retired = true
			host.Generation = 1
			record.Pending = 0
			return writeHostRecord(transaction.Bucket(hostsBucket), prefix, page.Host, host)
		},
	},
}

func TestHostPageRemovalChunkRejectsMissingAndCorruptState(t *testing.T) {
	for _, testCase := range hostPageRemovalFaults {
		t.Run(testCase.name, func(t *testing.T) {
			runHostPageRemovalFault(t, testCase)
		})
	}
}

func runHostPageRemovalFault(t *testing.T, testCase hostPageRemovalFault) {
	t.Helper()
	checkpoint, provenance, page := admittedCheckpoint(t)
	prefix, _ := provenancePrefix(provenance)
	mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
		record, _, err := readRunRecord(transaction, provenance)
		if err != nil {
			return err
		}
		if err := testCase.mutate(transaction, provenance, prefix, page, &record); err != nil {
			return err
		}
		if transaction.Bucket(runsBucket).Get(provenance) == nil {
			return nil
		}
		return writeRunRecord(transaction, provenance, record)
	})
	current, err := checkpoint.removeHostPagesChunk(testContext, hostPageRemoval{
		provenance: provenance,
		prefix:     prefix,
		host:       page.Host,
		generation: 1,
		pageURLs:   []string{page.URL},
	})
	if current && testCase.name != "page removal" && testCase.name != "pending" {
		t.Fatal("corrupt removal reported current")
	}
	if err == nil {
		t.Fatal("corrupt host-page removal succeeded")
	}
}

func TestHostPageRemovalChunksPropagateMissingRun(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	if err := checkpoint.removeHostPagesInChunks(testContext, hostPageRemoval{
		provenance: []byte(
			"missing",
		),
		prefix:   []byte{1},
		host:     "missing.example",
		pageURLs: []string{"https://missing.example/"},
	}); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("chunked missing run error = %v", err)
	}
}

func TestRetiredHostTransitionDiscoveryValidatesHostRowsAndProvenance(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*bolt.Tx) error
	}{
		{name: "empty host", mutate: func(transaction *bolt.Tx) error {
			provenance := []byte("empty-host-run")
			record := runRecord{OrderIdentity: []byte("identity")}
			if err := writeRunRecord(transaction, provenance, record); err != nil {
				return err
			}
			prefix, _ := provenancePrefix(provenance)
			encoded, _ := encodeRow("host", hostRecord{Retired: true})
			return transaction.Bucket(hostsBucket).Put(prefix, encoded)
		}},
		{name: "host encoding", mutate: func(transaction *bolt.Tx) error {
			provenance := []byte("bad-host-run")
			if err := writeRunRecord(transaction, provenance, runRecord{OrderIdentity: []byte("identity")}); err != nil {
				return err
			}
			prefix, _ := provenancePrefix(provenance)
			return transaction.Bucket(hostsBucket).Put(childRowKey(prefix, "bad.example"), []byte("{"))
		}},
		{name: "provenance", mutate: func(transaction *bolt.Tx) error {
			provenance := []byte(strings.Repeat("p", (bolt.MaxKeySize-2)/2+1))
			if err := writeRunRecord(transaction, provenance, runRecord{OrderIdentity: []byte("identity")}); err != nil {
				return err
			}
			return transaction.Bucket(hostsBucket).Put(provenance, []byte("{}"))
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
			mutateCheckpoint(t, checkpoint, testCase.mutate)
			if err := checkpoint.resumeRetiredHostTransitions(
				testContext,
			); !errors.Is(
				err,
				ErrCorruptCheckpoint,
			) {
				t.Fatalf("retired transition discovery error = %v", err)
			}
		})
	}
}

func TestResumeRetiredHostTransitionHandlesTerminalAndCorruptRuns(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	for _, testCase := range []struct {
		name       string
		provenance []byte
		prepare    func(*testing.T, *FrontierCheckpoint, []byte)
		wantDone   bool
		wantErr    error
	}{
		{name: "missing", provenance: []byte("missing"), prepare: func(*testing.T, *FrontierCheckpoint, []byte) {}, wantDone: true},
		{name: "completed", provenance: []byte("terminal"), prepare: func(t *testing.T, checkpoint *FrontierCheckpoint, provenance []byte) {
			beginTestRun(t, checkpoint, provenance, []byte("identity"))
			if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
				t.Fatalf("finish retired terminal fixture: %v", err)
			}
		}, wantDone: true},
		{name: "corrupt", provenance: []byte("corrupt"), prepare: func(t *testing.T, checkpoint *FrontierCheckpoint, provenance []byte) {
			mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
				return transaction.Bucket(runsBucket).Put(provenance, []byte("{"))
			})
		}, wantErr: ErrCorruptCheckpoint},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.prepare(t, checkpoint, testCase.provenance)
			prefix, _ := provenancePrefix(testCase.provenance)
			done, err := checkpoint.resumeRetiredHostTransitionChunk(
				testContext, testCase.provenance, prefix, "host.example",
			)
			if done != testCase.wantDone || !errors.Is(err, testCase.wantErr) {
				t.Fatalf("retired terminal transition = %v, %v", done, err)
			}
		})
	}
}

func TestResumeRetiredHostTransitionRejectsMissingBucketAndIgnoresCurrentHost(t *testing.T) {
	t.Run("missing bucket", func(t *testing.T) {
		checkpoint, provenance, page := admittedCheckpoint(t)
		prefix, _ := provenancePrefix(provenance)
		deleteSchemaBucket(t, checkpoint, visitedBucket)
		if _, err := checkpoint.resumeRetiredHostTransitionChunk(
			testContext, provenance, prefix, page.Host,
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("missing retirement bucket error = %v", err)
		}
	})
	t.Run("current host", func(t *testing.T) {
		checkpoint, provenance, page := admittedCheckpoint(t)
		prefix, _ := provenancePrefix(provenance)
		done, err := checkpoint.resumeRetiredHostTransitionChunk(
			testContext, provenance, prefix, page.Host,
		)
		if err != nil || !done {
			t.Fatalf("current host transition = %v, %v", done, err)
		}
	})
}

func TestReadOnlyRetirementTransitionPropagatesRemovalAndHostWrites(t *testing.T) {
	t.Run("removal", func(t *testing.T) {
		checkpoint, provenance, page := admittedCheckpoint(t)
		prefix, _ := provenancePrefix(provenance)
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			host, err := readHostRecord(transaction.Bucket(hostsBucket), prefix, page.Host)
			if err != nil {
				return err
			}
			host.Retired = true
			return writeHostRecord(transaction.Bucket(hostsBucket), prefix, page.Host, host)
		})
		if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
			_, err := resumeRetiredHostTransition(transaction, provenance, prefix, page.Host)
			return err
		}); err == nil {
			t.Fatal("read-only retirement removal succeeded")
		}
	})
	t.Run("host state", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		provenance := []byte("empty-retirement-write")
		beginTestRun(t, checkpoint, provenance, []byte("identity"))
		prefix, _ := provenancePrefix(provenance)
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return writeHostRecord(
				transaction.Bucket(hostsBucket), prefix, "empty.example", hostRecord{Retired: true},
			)
		})
		if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
			_, err := resumeRetiredHostTransition(transaction, provenance, prefix, "empty.example")
			return err
		}); err == nil {
			t.Fatal("read-only retirement host write succeeded")
		}
	})
}

func TestRetiredHostPageChunkRejectsInvalidPersistedPage(t *testing.T) {
	checkpoint, provenance, page := admittedCheckpoint(t)
	prefix, _ := provenancePrefix(provenance)
	page.Host = ""
	encoded, _ := encodeRow("page", page)
	mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
		return transaction.Bucket(pagesBucket).Put(sequenceRowKey(prefix, 1), encoded)
	})
	if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
		_, _, _, err := retiredHostPageChunk(
			transaction.Bucket(pagesBucket), prefix, "example.com", 0,
		)
		return err
	}); !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("invalid retired page error = %v", err)
	}
}

func TestCancelledRunTransitionResidualFailures(t *testing.T) {
	t.Run("missing bucket", func(t *testing.T) {
		checkpoint, provenance, page := admittedCheckpoint(t)
		prefix, _ := provenancePrefix(provenance)
		mutateRunRecord(
			t,
			checkpoint,
			provenance,
			func(record *runRecord) { record.Cancelled = true },
		)
		deleteSchemaBucket(t, checkpoint, visitedBucket)
		if err := checkpoint.cancelQueuedPageChunk(
			testContext, provenance, prefix, []string{page.URL},
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("missing cancellation bucket error = %v", err)
		}
	})
	t.Run("page removal", func(t *testing.T) {
		checkpoint, provenance, page := admittedCheckpoint(t)
		prefix, _ := provenancePrefix(provenance)
		mutateRunRecord(
			t,
			checkpoint,
			provenance,
			func(record *runRecord) { record.Cancelled = true },
		)
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return transaction.Bucket(pagePositionsBucket).
				Put(childRowKey(prefix, page.URL), []byte{1})
		})
		if err := checkpoint.cancelQueuedPageChunk(
			testContext, provenance, prefix, []string{page.URL},
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("cancellation removal error = %v", err)
		}
	})
	t.Run("pending", func(t *testing.T) {
		checkpoint, provenance, page := admittedCheckpoint(t)
		prefix, _ := provenancePrefix(provenance)
		mutateRunRecord(t, checkpoint, provenance, func(record *runRecord) {
			record.Cancelled = true
			record.Pending = 0
		})
		if err := checkpoint.cancelQueuedPageChunk(
			testContext, provenance, prefix, []string{page.URL},
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("cancellation pending error = %v", err)
		}
	})
	t.Run("residual pending", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		provenance := []byte("cancel-residual-pending")
		beginTestRun(t, checkpoint, provenance, []byte("identity"))
		mutateRunRecord(t, checkpoint, provenance, func(record *runRecord) {
			record.Cancelled = true
			record.Pending = 1
		})
		prefix, _ := provenancePrefix(provenance)
		if _, err := checkpoint.resumeCancelledRunChunk(
			testContext, provenance, prefix,
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("residual cancellation pending error = %v", err)
		}
	})
	t.Run("zero manifest window", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
			deleted, err := cancelManifestRows(transaction, []byte{1}, 0)
			if err != nil || deleted != 0 {
				t.Fatalf("zero manifest cancellation = %d, %v", deleted, err)
			}
			return nil
		}); err != nil {
			t.Fatalf("inspect zero manifest cancellation: %v", err)
		}
	})
}

func TestResumeCancelledRunsPropagatesTransitionFailure(t *testing.T) {
	checkpoint, provenance, page := admittedCheckpoint(t)
	mutateRunRecord(t, checkpoint, provenance, func(record *runRecord) { record.Cancelled = true })
	prefix, _ := provenancePrefix(provenance)
	mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
		return transaction.Bucket(pagePositionsBucket).Put(childRowKey(prefix, page.URL), []byte{1})
	})
	if err := checkpoint.resumeCancelledRuns(testContext); !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("cancelled transition failure = %v", err)
	}
}
