package frontiercheckpoint

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlsettlement"
)

func TestOpenPropagatesEveryUnfinishedTransitionFailure(t *testing.T) {
	testOpenSeedTransitionFailure(t)
	testOpenCancellationTransitionFailure(t)
	testOpenRetirementTransitionFailure(t)
}

func testOpenSeedTransitionFailure(t *testing.T) {
	t.Helper()
	t.Run("seed manifest", func(t *testing.T) {
		path := testCheckpointPath(t)
		checkpoint := openTestCheckpoint(t, path)
		if err := checkpoint.Close(); err != nil {
			t.Fatalf("close seed transition fixture: %v", err)
		}
		writeRawCheckpoint(t, path, func(transaction *bolt.Tx) error {
			provenance := []byte(strings.Repeat("p", (bolt.MaxKeySize-2)/2+1))
			encoded, err := encodeRow("run", runRecord{
				OrderIdentity:          []byte("identity"),
				SeedManifestPublishing: true,
			})
			if err != nil {
				return err
			}
			return transaction.Bucket(runsBucket).Put(provenance, encoded)
		})
		requireOpenError(t, path, ErrCorruptCheckpoint)
	})
}

func testOpenCancellationTransitionFailure(t *testing.T) {
	t.Helper()
	t.Run("cancelled run", func(t *testing.T) {
		path := testCheckpointPath(t)
		checkpoint := openTestCheckpoint(t, path)
		provenance := []byte("open-cancel-transition")
		beginTestRun(t, checkpoint, provenance, []byte("identity"))
		page := testPage("https://cancel.example/page", "cancel.example", "cancel", 0)
		if admitted, err := checkpoint.Admit(
			testContext,
			provenance,
			[]Page{page},
		); err != nil ||
			admitted != 1 {
			t.Fatalf("admit cancellation fixture = %d, %v", admitted, err)
		}
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
			return transaction.Bucket(pagePositionsBucket).
				Put(childRowKey(prefix, page.URL), []byte{1})
		})
		if err := checkpoint.Close(); err != nil {
			t.Fatalf("close cancellation transition fixture: %v", err)
		}
		requireOpenError(t, path, ErrCorruptCheckpoint)
	})
}

func testOpenRetirementTransitionFailure(t *testing.T) {
	t.Helper()
	t.Run("retired host", func(t *testing.T) {
		path := testCheckpointPath(t)
		checkpoint := openTestCheckpoint(t, path)
		provenance := []byte("open-retirement-transition")
		beginTestRun(t, checkpoint, provenance, []byte("identity"))
		page := testPage("https://retired.example/page", "retired.example", "retired", 0)
		if admitted, err := checkpoint.Admit(
			testContext,
			provenance,
			[]Page{page},
		); err != nil ||
			admitted != 1 {
			t.Fatalf("admit retirement fixture = %d, %v", admitted, err)
		}
		prefix, _ := provenancePrefix(provenance)
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			host, err := readHostRecord(transaction.Bucket(hostsBucket), prefix, page.Host)
			if err != nil {
				return err
			}
			host.Retired = true
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
		})
		if err := checkpoint.Close(); err != nil {
			t.Fatalf("close retirement transition fixture: %v", err)
		}
		requireOpenError(t, path, ErrCorruptCheckpoint)
	})
}

func TestHostPageValidationPropagatesRunBucketAndTransitionFailures(t *testing.T) {
	t.Run("missing run", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		provenance := []byte("missing-host-validation")
		prefix, _ := provenancePrefix(provenance)
		if err := checkpoint.validateHostPages(
			testContext, provenance, prefix, "host.example", nil,
		); !errors.Is(err, ErrRunNotFound) {
			t.Fatalf("missing host-validation run error = %v", err)
		}
	})
	t.Run("missing bucket", func(t *testing.T) {
		checkpoint, provenance, _ := admittedCheckpoint(t)
		prefix, _ := provenancePrefix(provenance)
		deleteSchemaBucket(t, checkpoint, visitedBucket)
		if err := checkpoint.validateHostPages(
			testContext, provenance, prefix, "host.example", nil,
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("missing host-validation bucket error = %v", err)
		}
	})
	t.Run("retirement transition", func(t *testing.T) {
		checkpoint, provenance, page := admittedCheckpoint(t)
		prefix, _ := provenancePrefix(provenance)
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			host, err := readHostRecord(transaction.Bucket(hostsBucket), prefix, page.Host)
			if err != nil {
				return err
			}
			host.Retired = true
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
		})
		if err := checkpoint.resumeRetiredHostTransitions(
			testContext,
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("retirement transition error = %v", err)
		}
	})
}

func TestSeedTransitionLoopsPropagateChunkAndStorageFailures(t *testing.T) {
	testSeedPublicationLoopFailure(t)
	testSeedDeletionLoopFailure(t)
	testCorruptSeedTransitionRuns(t)
	testSeedManifestRowWriteFailure(t)
}

func testSeedPublicationLoopFailure(t *testing.T) {
	t.Helper()
	t.Run("publication loop", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		page := testPage(
			"https://publication.example/page",
			"publication.example",
			"publication",
			0,
		)
		encoded, err := encodeSeedManifest([]Page{page})
		if err != nil {
			t.Fatalf("encode publication fixture: %v", err)
		}
		publication := testSeedManifestPublication(
			t, []byte("resume-publication-failure"), []byte("identity"), encoded,
		)
		if _, err := checkpoint.prepareSeedManifestPublication(
			testContext, publication,
		); err != nil {
			t.Fatalf("prepare publication transition: %v", err)
		}
		deleteSchemaBucket(t, checkpoint, seedManifestBucket)
		if err := checkpoint.resumeSeedManifestTransitions(
			testContext,
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("publication transition error = %v", err)
		}
	})
}

func testSeedDeletionLoopFailure(t *testing.T) {
	t.Helper()
	t.Run("deletion loop", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		provenance := []byte("resume-deletion-failure")
		beginSeedManifest(t, checkpoint, provenance, []Page{
			testPage("https://deletion.example/page", "deletion.example", "deletion", 0),
		})
		mutateRunRecord(t, checkpoint, provenance, func(record *runRecord) {
			record.SeedManifest = false
			record.SeedManifestDeleting = true
		})
		deleteSchemaBucket(t, checkpoint, seedManifestBucket)
		if err := checkpoint.resumeSeedManifestTransitions(
			testContext,
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("deletion transition error = %v", err)
		}
	})
}

func testCorruptSeedTransitionRuns(t *testing.T) {
	t.Helper()
	for _, discard := range []bool{false, true} {
		name := "consumed"
		if discard {
			name = "publication"
		}
		t.Run(name+" corrupt run", func(t *testing.T) {
			checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
			provenance := []byte("corrupt-" + name)
			prefix, _ := provenancePrefix(provenance)
			mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
				return transaction.Bucket(runsBucket).Put(provenance, []byte("{"))
			})
			var err error
			if discard {
				_, err = checkpoint.discardSeedManifestPublicationChunk(
					testContext, provenance, prefix,
				)
			} else {
				_, err = checkpoint.deleteConsumedSeedManifestChunk(
					testContext, provenance, prefix,
				)
			}
			if !errors.Is(err, ErrCorruptCheckpoint) {
				t.Fatalf("corrupt %s transition error = %v", name, err)
			}
		})
	}
}

func testSeedManifestRowWriteFailure(t *testing.T) {
	t.Helper()
	t.Run("manifest row write", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		page := testPage("https://row.example/page", "row.example", "row", 0)
		encoded, err := encodeSeedManifest([]Page{page})
		if err != nil {
			t.Fatalf("encode row fixture: %v", err)
		}
		publication := testSeedManifestPublication(
			t, []byte("read-only-manifest-row"), []byte("identity"), encoded,
		)
		if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
			return writeSeedManifestRows(
				transaction.Bucket(seedManifestBucket), publication, 0, 1,
			)
		}); err == nil {
			t.Fatal("read-only manifest row write succeeded")
		}
	})
}

func TestTerminalSettlementReadsAndAcknowledgmentPropagateCorruption(t *testing.T) {
	t.Run("acknowledgment row", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		settlement := completedTerminalSettlement(t, checkpoint, "ack-row-corruption")
		if err := checkpoint.Stage(testContext, settlement); err != nil {
			t.Fatalf("stage acknowledgment fixture: %v", err)
		}
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return transaction.Bucket(terminalOutboxBucket).
				Put([]byte(settlement.LeaseID), []byte("{"))
		})
		if err := checkpoint.RecordAcknowledgment(
			testContext,
			settlement.LeaseID,
			settlement.OrderIdentity,
			bytes.Repeat([]byte{1}, sha256.Size),
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("corrupt acknowledgment row error = %v", err)
		}
	})
	for _, testCase := range []struct {
		name   string
		mutate func(*bolt.Tx, crawlsettlement.Settlement) error
	}{
		{
			name: "missing bucket",
			mutate: func(transaction *bolt.Tx, _ crawlsettlement.Settlement) error {
				return transaction.DeleteBucket(terminalOutboxBucket)
			},
		},
		{
			name: "corrupt row",
			mutate: func(transaction *bolt.Tx, settlement crawlsettlement.Settlement) error {
				return transaction.Bucket(terminalOutboxBucket).Put([]byte(settlement.LeaseID), []byte("{"))
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
			settlement := completedTerminalSettlement(t, checkpoint, "read-"+testCase.name)
			if err := checkpoint.Stage(testContext, settlement); err != nil {
				t.Fatalf("stage read fixture: %v", err)
			}
			mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
				return testCase.mutate(transaction, settlement)
			})
			if _, _, err := checkpoint.Current(
				testContext, settlement.LeaseID, settlement.OrderIdentity,
			); !errors.Is(err, ErrCorruptCheckpoint) {
				t.Fatalf("terminal read corruption error = %v", err)
			}
		})
	}
}

func TestPrepareConfirmationPropagatesInitialReadAndDeletionFailures(t *testing.T) {
	t.Run("initial read", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		settlement := completedTerminalSettlement(t, checkpoint, "prepare-read")
		if err := checkpoint.Stage(testContext, settlement); err != nil {
			t.Fatalf("stage prepare-read fixture: %v", err)
		}
		deleteSchemaBucket(t, checkpoint, terminalOutboxBucket)
		if err := checkpoint.PrepareConfirmation(
			testContext, settlement.LeaseID, settlement.OrderIdentity,
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("prepare initial read error = %v", err)
		}
	})
	t.Run("run deletion", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		settlement := completedTerminalSettlement(t, checkpoint, "prepare-delete")
		if err := checkpoint.Stage(testContext, settlement); err != nil {
			t.Fatalf("stage prepare-delete fixture: %v", err)
		}
		token := bytes.Repeat([]byte{2}, sha256.Size)
		if err := checkpoint.RecordAcknowledgment(
			testContext, settlement.LeaseID, settlement.OrderIdentity, token,
		); err != nil {
			t.Fatalf("acknowledge prepare-delete fixture: %v", err)
		}
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return transaction.Bucket(runsBucket).Put(settlement.Provenance, []byte("{"))
		})
		if err := checkpoint.PrepareConfirmation(
			testContext, settlement.LeaseID, settlement.OrderIdentity,
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("prepare deletion error = %v", err)
		}
	})
}
