package frontiercheckpoint

import (
	"errors"
	"math"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestCancellationRejectsInvalidRequestsAndUnmarkedRuns(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("cancel-invalid")
	beginTestRun(t, checkpoint, provenance, []byte("identity"))
	for _, testCase := range []struct {
		name       string
		provenance []byte
		pages      []string
		want       error
	}{
		{name: "provenance", pages: []string{"https://example.com/"}, want: ErrInvalidProvenance},
		{name: "blank page", provenance: provenance, pages: []string{" "}, want: ErrInvalidPage},
		{name: "unmarked empty", provenance: provenance, want: ErrCorruptCheckpoint},
		{name: "unmarked page", provenance: provenance, pages: []string{"https://example.com/"}, want: ErrCorruptCheckpoint},
		{name: "missing", provenance: []byte("missing"), pages: []string{"https://example.com/"}, want: ErrRunNotFound},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if err := checkpoint.CancelQueuedPages(
				testContext, testCase.provenance, testCase.pages,
			); !errors.Is(err, testCase.want) {
				t.Fatalf("cancel queued error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestCancellationChunksRemoveOnlyPersistedPages(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("cancel-chunks")
	beginTestRun(t, checkpoint, provenance, []byte("identity"))
	pages := boundedRecoveryTestPages(cancellationPagesPerTransaction + 2)
	admitCheckpointTestPages(t, checkpoint, provenance, pages)
	if err := checkpoint.UpdateControl(
		testContext,
		provenance,
		ControlUpdate{Cancelled: true},
	); err != nil {
		t.Fatalf("mark cancellation: %v", err)
	}
	urls := make([]string, 0, len(pages)+1)
	for _, page := range pages {
		urls = append(urls, page.URL)
	}
	urls = append(urls, "https://missing.example/page")
	if err := checkpoint.CancelQueuedPages(testContext, provenance, urls); err != nil {
		t.Fatalf("cancel queued chunks: %v", err)
	}
	state, err := checkpoint.Inspect(testContext, provenance, []byte("identity"))
	if err != nil || state.Pending != 0 {
		t.Fatalf("cancelled chunk state = %+v, %v", state, err)
	}
}

func TestCancelledRunDiscoveryRejectsCorruptRows(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*bolt.Tx) error
	}{
		{name: "missing runs", mutate: func(transaction *bolt.Tx) error {
			return transaction.DeleteBucket(runsBucket)
		}},
		{name: "encoding", mutate: func(transaction *bolt.Tx) error {
			return transaction.Bucket(runsBucket).Put([]byte("broken"), []byte("{"))
		}},
		{name: "identity", mutate: func(transaction *bolt.Tx) error {
			encoded, err := encodeRow("run", runRecord{})
			if err != nil {
				return err
			}
			return transaction.Bucket(runsBucket).Put([]byte("broken"), encoded)
		}},
		{name: "provenance", mutate: func(transaction *bolt.Tx) error {
			encoded, err := encodeRow("run", runRecord{OrderIdentity: []byte("identity"), Cancelled: true})
			if err != nil {
				return err
			}
			return transaction.Bucket(runsBucket).Put([]byte(strings.Repeat("p", (bolt.MaxKeySize-2)/2+1)), encoded)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
			mutateCheckpoint(t, checkpoint, testCase.mutate)
			if err := checkpoint.resumeCancelledRuns(
				testContext,
			); !errors.Is(
				err,
				ErrCorruptCheckpoint,
			) {
				t.Fatalf("cancelled discovery error = %v", err)
			}
		})
	}
}

func TestCancelledTransitionRejectsCorruptFrontierState(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*bolt.Tx, []byte, Page, *runRecord) error
	}{
		{name: "unmarked", mutate: func(_ *bolt.Tx, _ []byte, _ Page, _ *runRecord) error { return nil }},
		{name: "missing bucket", mutate: func(transaction *bolt.Tx, _ []byte, _ Page, record *runRecord) error {
			record.Cancelled = true
			return transaction.DeleteBucket(visitedBucket)
		}},
		{name: "page key", mutate: func(transaction *bolt.Tx, prefix []byte, _ Page, record *runRecord) error {
			record.Cancelled = true
			if err := transaction.Bucket(pagesBucket).Delete(sequenceRowKey(prefix, 1)); err != nil {
				return wrapDatabaseError("delete cancellation page fixture", err)
			}
			return transaction.Bucket(pagesBucket).Put(childRowKey(prefix, "bad"), []byte("{}"))
		}},
		{name: "page encoding", mutate: func(transaction *bolt.Tx, prefix []byte, _ Page, record *runRecord) error {
			record.Cancelled = true
			return transaction.Bucket(pagesBucket).Put(sequenceRowKey(prefix, 1), []byte("{"))
		}},
		{name: "page fields", mutate: func(transaction *bolt.Tx, prefix []byte, page Page, record *runRecord) error {
			record.Cancelled = true
			page.Host = ""
			encoded, _ := encodeRow("page", page)
			return transaction.Bucket(pagesBucket).Put(sequenceRowKey(prefix, 1), encoded)
		}},
		{name: "pending", mutate: func(_ *bolt.Tx, _ []byte, _ Page, record *runRecord) error {
			record.Cancelled = true
			record.Pending = 0
			return nil
		}},
		{name: "manifest bucket", mutate: func(transaction *bolt.Tx, _ []byte, _ Page, record *runRecord) error {
			record.Cancelled = true
			if err := transaction.Bucket(pagesBucket).ForEach(func(key, _ []byte) error {
				return transaction.Bucket(pagesBucket).Delete(key)
			}); err != nil {
				return wrapDatabaseError("clear cancellation pages fixture", err)
			}
			record.Pending = 0
			return transaction.DeleteBucket(seedManifestBucket)
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
			if _, err := checkpoint.resumeCancelledRunChunk(
				testContext, provenance, prefix,
			); !errors.Is(err, ErrCorruptCheckpoint) {
				t.Fatalf("cancelled transition error = %v", err)
			}
		})
	}
}

func TestCancelledTransitionTreatsMissingAndTerminalRunsAsSettled(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	for _, testCase := range []struct {
		name       string
		provenance []byte
		prepare    func(*testing.T, *FrontierCheckpoint, []byte)
	}{
		{name: "missing", provenance: []byte("missing"), prepare: func(*testing.T, *FrontierCheckpoint, []byte) {}},
		{name: "completed", provenance: []byte("completed"), prepare: func(t *testing.T, checkpoint *FrontierCheckpoint, provenance []byte) {
			beginTestRun(t, checkpoint, provenance, []byte("completed-identity"))
			if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
				t.Fatalf("finish completed cancellation fixture: %v", err)
			}
		}},
		{name: "deleting", provenance: []byte("deleting"), prepare: func(t *testing.T, checkpoint *FrontierCheckpoint, provenance []byte) {
			beginTestRun(t, checkpoint, provenance, []byte("deleting-identity"))
			mutateRunRecord(t, checkpoint, provenance, func(record *runRecord) { record.Deleting = true })
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.prepare(t, checkpoint, testCase.provenance)
			prefix, _ := provenancePrefix(testCase.provenance)
			done, err := checkpoint.resumeCancelledRunChunk(
				testContext,
				testCase.provenance,
				prefix,
			)
			if err != nil || !done {
				t.Fatalf("terminal cancellation transition = %v, %v", done, err)
			}
		})
	}
}

func TestHostRetirementValidationAndChunkFencing(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("host-retirement-fence")
	beginTestRun(t, checkpoint, provenance, []byte("identity"))
	page := testPage("https://retire.example/page", "retire.example", "retire", 0)
	if _, err := checkpoint.Admit(testContext, provenance, []Page{page}); err != nil {
		t.Fatalf("admit retirement page: %v", err)
	}
	prefix, _ := provenancePrefix(provenance)
	if err := checkpoint.validateHostPages(
		testContext,
		provenance,
		prefix,
		"retire.example",
		[]string{page.URL, page.URL, "https://missing.example/"},
	); err != nil {
		t.Fatalf("validate duplicate retirement pages: %v", err)
	}
	if err := checkpoint.validateHostPages(
		testContext, provenance, prefix, "other.example", []string{page.URL},
	); !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("retirement host mismatch error = %v", err)
	}
	current, err := checkpoint.removeHostPagesChunk(testContext, hostPageRemoval{
		provenance: provenance,
		prefix:     prefix,
		host:       page.Host,
		generation: 9,
		pageURLs:   []string{page.URL},
	})
	if err != nil || current {
		t.Fatalf("stale retirement chunk = %v, %v", current, err)
	}
}

func TestHostRetirementDiscoveryRejectsCorruptRows(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*bolt.Tx) error
	}{
		{name: "missing runs", mutate: func(transaction *bolt.Tx) error { return transaction.DeleteBucket(runsBucket) }},
		{name: "missing hosts", mutate: func(transaction *bolt.Tx) error { return transaction.DeleteBucket(hostsBucket) }},
		{name: "run encoding", mutate: func(transaction *bolt.Tx) error {
			return transaction.Bucket(runsBucket).Put([]byte("broken"), []byte("{"))
		}},
		{name: "provenance", mutate: func(transaction *bolt.Tx) error {
			encoded, err := encodeRow("run", runRecord{OrderIdentity: []byte("identity")})
			if err != nil {
				return err
			}
			return transaction.Bucket(runsBucket).Put([]byte(strings.Repeat("p", (bolt.MaxKeySize-2)/2+1)), encoded)
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
				t.Fatalf("retirement discovery error = %v", err)
			}
		})
	}
}

func TestHostRetirementTransitionRejectsCorruptState(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*bolt.Tx, []byte, Page, *runRecord) error
	}{
		{name: "host encoding", mutate: func(transaction *bolt.Tx, prefix []byte, page Page, _ *runRecord) error {
			return transaction.Bucket(hostsBucket).Put(childRowKey(prefix, page.Host), []byte("{"))
		}},
		{name: "cursor", mutate: func(transaction *bolt.Tx, prefix []byte, page Page, record *runRecord) error {
			host, err := readHostRecord(transaction.Bucket(hostsBucket), prefix, page.Host)
			if err != nil {
				return err
			}
			host.Retired = true
			host.RetirementCursor = record.NextSequence + 1
			return writeHostRecord(transaction.Bucket(hostsBucket), prefix, page.Host, host)
		}},
		{name: "page encoding", mutate: func(transaction *bolt.Tx, prefix []byte, page Page, _ *runRecord) error {
			host, err := readHostRecord(transaction.Bucket(hostsBucket), prefix, page.Host)
			if err != nil {
				return err
			}
			host.Retired = true
			if err := writeHostRecord(transaction.Bucket(hostsBucket), prefix, page.Host, host); err != nil {
				return err
			}
			return transaction.Bucket(pagesBucket).Put(sequenceRowKey(prefix, 1), []byte("{"))
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
			if _, err := checkpoint.resumeRetiredHostTransitionChunk(
				testContext, provenance, prefix, page.Host,
			); !errors.Is(err, ErrCorruptCheckpoint) {
				t.Fatalf("retirement transition error = %v", err)
			}
		})
	}
}

func TestRetiredHostPageChunkRejectsOverflowAndCorruptRows(t *testing.T) {
	checkpoint, provenance, page := admittedCheckpoint(t)
	prefix, _ := provenancePrefix(provenance)
	if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
		pages := transaction.Bucket(pagesBucket)
		urls, cursor, done, err := retiredHostPageChunk(pages, prefix, page.Host, math.MaxUint64)
		if err != nil || !done || cursor != math.MaxUint64 || len(urls) != 0 {
			t.Fatalf("maximum retirement cursor = %v, %d, %v, %v", urls, cursor, done, err)
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect retirement page chunk: %v", err)
	}
	mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
		if err := transaction.Bucket(pagesBucket).Delete(sequenceRowKey(prefix, 1)); err != nil {
			return wrapDatabaseError("delete retirement page fixture", err)
		}
		return transaction.Bucket(pagesBucket).Put(childRowKey(prefix, "bad"), []byte("{}"))
	})
	if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
		_, _, _, err := retiredHostPageChunk(transaction.Bucket(pagesBucket), prefix, page.Host, 0)
		return err
	}); !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("corrupt retirement page key error = %v", err)
	}
}
