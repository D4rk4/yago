package frontiercheckpoint

import (
	"errors"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestSeedManifestRejectsUnencodableTimeAndExcessDecision(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	page := testPage("https://seed.example/page", "seed.example", "seed", 0)
	page.ObservedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := checkpoint.BeginSeedManifest(
		testContext,
		[]byte("unencodable-seed"),
		[]byte("identity"),
		yagocrawlcontract.CrawlOrderPriorityNormal,
		[]Page{page},
	); err == nil {
		t.Fatal("seed manifest with unencodable time succeeded")
	}
	if validSeedBatchCursor(runRecord{SeedLength: 1}, SeedBatch{
		Decisions: []SeedDecision{{}, {}},
	}) {
		t.Fatal("seed batch accepted more decisions than manifest rows")
	}
}

func TestSeedPublicationReplayAndCompletionAreIdempotent(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	page := testPage("https://publication.example/page", "publication.example", "publication", 0)
	encoded, err := encodeSeedManifest([]Page{page})
	if err != nil {
		t.Fatalf("encode publication fixture: %v", err)
	}
	publication := testSeedManifestPublication(
		t,
		[]byte("publication-replay"),
		[]byte("identity"),
		encoded,
	)
	publishing, err := checkpoint.prepareSeedManifestPublication(testContext, publication)
	if err != nil || !publishing {
		t.Fatalf("prepare publication = %v, %v", publishing, err)
	}
	publishing, err = checkpoint.prepareSeedManifestPublication(testContext, publication)
	if err != nil || !publishing {
		t.Fatalf("replay publication = %v, %v", publishing, err)
	}
	done, err := checkpoint.stageSeedManifestChunk(testContext, publication)
	if err != nil || !done {
		t.Fatalf("stage publication = %v, %v", done, err)
	}
	done, err = checkpoint.stageSeedManifestChunk(testContext, publication)
	if err != nil || !done {
		t.Fatalf("replay staged publication = %v, %v", done, err)
	}
	if err := checkpoint.completeSeedManifestPublication(
		testContext,
		publication.provenance,
		publication.manifestIdentity,
		publication.manifestLength,
	); err != nil {
		t.Fatalf("complete publication: %v", err)
	}
	if err := checkpoint.completeSeedManifestPublication(
		testContext,
		publication.provenance,
		publication.manifestIdentity,
		publication.manifestLength,
	); err != nil {
		t.Fatalf("replay completed publication: %v", err)
	}
	done, err = checkpoint.stageSeedManifestChunk(testContext, publication)
	if err != nil || !done {
		t.Fatalf("stage completed publication = %v, %v", done, err)
	}
}

type seedPublicationFault struct {
	name   string
	mutate func(*bolt.Tx, seedManifestPublication, *runRecord) error
}

var seedPublicationFaults = []seedPublicationFault{
	{
		name: "missing run",
		mutate: func(transaction *bolt.Tx, publication seedManifestPublication, _ *runRecord) error {
			return transaction.Bucket(runsBucket).Delete(publication.provenance)
		},
	},
	{
		name: "changed identity",
		mutate: func(_ *bolt.Tx, _ seedManifestPublication, record *runRecord) error {
			record.SeedManifestIdentity = []byte("changed")
			return nil
		},
	},
	{name: "cursor", mutate: func(_ *bolt.Tx, _ seedManifestPublication, record *runRecord) error {
		record.SeedCursor = record.SeedLength + 1
		return nil
	}},
	{
		name: "missing manifest",
		mutate: func(transaction *bolt.Tx, _ seedManifestPublication, _ *runRecord) error {
			return transaction.DeleteBucket(seedManifestBucket)
		},
	},
	{
		name: "overlap",
		mutate: func(transaction *bolt.Tx, publication seedManifestPublication, _ *runRecord) error {
			return transaction.Bucket(seedManifestBucket).Put(
				sequenceRowKey(publication.prefix, 1), publication.encodedPages[0],
			)
		},
	},
}

func TestSeedPublicationRejectsChangedAndCorruptStagingState(t *testing.T) {
	page := testPage("https://publication.example/page", "publication.example", "publication", 0)
	encoded, _ := encodeSeedManifest([]Page{page})
	for _, testCase := range seedPublicationFaults {
		t.Run(testCase.name, func(t *testing.T) {
			runSeedPublicationFault(t, testCase, encoded)
		})
	}
}

func runSeedPublicationFault(
	t *testing.T,
	testCase seedPublicationFault,
	encoded [][]byte,
) {
	t.Helper()
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	publication := testSeedManifestPublication(
		t, []byte("stage-"+testCase.name), []byte("identity"), encoded,
	)
	publishing, err := checkpoint.prepareSeedManifestPublication(testContext, publication)
	if err != nil || !publishing {
		t.Fatalf("prepare staging fixture = %v, %v", publishing, err)
	}
	mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
		record, _, err := readRunRecord(transaction, publication.provenance)
		if err != nil {
			return err
		}
		if err := testCase.mutate(transaction, publication, &record); err != nil {
			return err
		}
		if transaction.Bucket(runsBucket) == nil ||
			transaction.Bucket(runsBucket).Get(publication.provenance) == nil {
			return nil
		}
		return writeRunRecord(transaction, publication.provenance, record)
	})
	if _, err := checkpoint.stageSeedManifestChunk(testContext, publication); err == nil {
		t.Fatal("corrupt seed staging succeeded")
	}
}

func TestSeedPublicationRejectsIncompleteCompletion(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	page := testPage("https://completion.example/page", "completion.example", "completion", 0)
	encoded, _ := encodeSeedManifest([]Page{page})
	publication := testSeedManifestPublication(
		t,
		[]byte("incomplete-publication"),
		[]byte("identity"),
		encoded,
	)
	if _, err := checkpoint.prepareSeedManifestPublication(testContext, publication); err != nil {
		t.Fatalf("prepare incomplete publication: %v", err)
	}
	if err := checkpoint.completeSeedManifestPublication(
		testContext,
		publication.provenance,
		publication.manifestIdentity,
		publication.manifestLength,
	); !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("incomplete publication error = %v", err)
	}
	if err := checkpoint.completeSeedManifestPublication(
		testContext, []byte("missing"), publication.manifestIdentity, publication.manifestLength,
	); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("missing publication error = %v", err)
	}
}

func TestPrepareSeedingFinishRejectsPublishingAndTallyOverflow(t *testing.T) {
	t.Run("publishing", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		page := testPage("https://publishing.example/page", "publishing.example", "publishing", 0)
		encoded, _ := encodeSeedManifest([]Page{page})
		publication := testSeedManifestPublication(
			t,
			[]byte("finish-publishing"),
			[]byte("identity"),
			encoded,
		)
		if _, err := checkpoint.prepareSeedManifestPublication(
			testContext,
			publication,
		); err != nil {
			t.Fatalf("prepare publishing fixture: %v", err)
		}
		if _, err := checkpoint.prepareSeedingFinish(
			testContext, publication.provenance, publication.prefix, testRunTally(),
		); !errors.Is(err, ErrSeedManifestMissing) {
			t.Fatalf("publishing finish error = %v", err)
		}
	})
	t.Run("tally", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		provenance := []byte("finish-tally-overflow")
		beginTestRun(t, checkpoint, provenance, []byte("identity"))
		mutateRunRecord(t, checkpoint, provenance, func(record *runRecord) {
			record.Tally.Fetched = ^uint64(0)
		})
		prefix, _ := provenancePrefix(provenance)
		if _, err := checkpoint.prepareSeedingFinish(
			testContext,
			provenance,
			prefix,
			yagocrawlcontract.CrawlRunTally{Fetched: 1},
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("seeding tally overflow error = %v", err)
		}
	})
}

type seedManifestCleanupFault struct {
	name   string
	mutate func(*bolt.Tx, []byte, *runRecord) error
	done   bool
	want   error
}

var seedManifestCleanupFaults = []seedManifestCleanupFault{
	{
		name: "missing run",
		mutate: func(transaction *bolt.Tx, provenance []byte, _ *runRecord) error {
			return transaction.Bucket(runsBucket).Delete(provenance)
		},
		done: true,
	},
	{name: "consumed", mutate: func(_ *bolt.Tx, _ []byte, record *runRecord) error {
		record.SeedManifestDeleting = false
		record.SeedManifestConsumed = true
		return nil
	}, done: true},
	{name: "missing marker", mutate: func(_ *bolt.Tx, _ []byte, record *runRecord) error {
		record.SeedManifestDeleting = false
		return nil
	}, want: ErrCorruptCheckpoint},
	{
		name: "missing manifest bucket",
		mutate: func(transaction *bolt.Tx, _ []byte, _ *runRecord) error {
			return transaction.DeleteBucket(seedManifestBucket)
		},
		want: ErrCorruptCheckpoint,
	},
	{
		name: "missing pages bucket",
		mutate: func(transaction *bolt.Tx, _ []byte, _ *runRecord) error {
			if err := transaction.Bucket(seedManifestBucket).ForEach(func(key, _ []byte) error {
				return transaction.Bucket(seedManifestBucket).Delete(key)
			}); err != nil {
				return wrapDatabaseError("clear seed manifest fixture", err)
			}
			return transaction.DeleteBucket(pagesBucket)
		},
		want: ErrCorruptCheckpoint,
	},
}

func TestSeedManifestCleanupChunkHandlesMissingAndInvalidMarkers(t *testing.T) {
	for _, testCase := range seedManifestCleanupFaults {
		t.Run(testCase.name, func(t *testing.T) {
			runSeedManifestCleanupFault(t, testCase)
		})
	}
}

func runSeedManifestCleanupFault(t *testing.T, testCase seedManifestCleanupFault) {
	t.Helper()
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("cleanup-" + testCase.name)
	page := testPage("https://cleanup.example/page", "cleanup.example", "cleanup", 0)
	beginSeedManifest(t, checkpoint, provenance, []Page{page})
	mutateRunRecord(t, checkpoint, provenance, func(record *runRecord) {
		record.SeedManifest = false
		record.SeedManifestDeleting = true
	})
	prefix, _ := provenancePrefix(provenance)
	mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
		record, _, err := readRunRecord(transaction, provenance)
		if err != nil {
			return err
		}
		if err := testCase.mutate(transaction, provenance, &record); err != nil {
			return err
		}
		if transaction.Bucket(runsBucket).Get(provenance) == nil {
			return nil
		}
		return writeRunRecord(transaction, provenance, record)
	})
	done, err := checkpoint.deleteConsumedSeedManifestChunk(testContext, provenance, prefix)
	if done != testCase.done || !errors.Is(err, testCase.want) {
		t.Fatalf("cleanup chunk = %v, %v", done, err)
	}
}

func TestSeedManifestTransitionDiscoveryRejectsCorruptRunRows(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*bolt.Tx) error
	}{
		{name: "missing runs", mutate: func(transaction *bolt.Tx) error { return transaction.DeleteBucket(runsBucket) }},
		{name: "encoding", mutate: func(transaction *bolt.Tx) error {
			return transaction.Bucket(runsBucket).Put([]byte("broken"), []byte("{"))
		}},
		{name: "identity", mutate: func(transaction *bolt.Tx) error {
			encoded, _ := encodeRow("run", runRecord{})
			return transaction.Bucket(runsBucket).Put([]byte("broken"), encoded)
		}},
		{name: "conflict", mutate: func(transaction *bolt.Tx) error {
			encoded, _ := encodeRow("run", runRecord{
				OrderIdentity: []byte("identity"), SeedManifestPublishing: true, SeedManifestDeleting: true,
			})
			return transaction.Bucket(runsBucket).Put([]byte("broken"), encoded)
		}},
		{name: "publication provenance", mutate: func(transaction *bolt.Tx) error {
			encoded, _ := encodeRow("run", runRecord{
				OrderIdentity: []byte("identity"), SeedManifestPublishing: true,
			})
			return transaction.Bucket(runsBucket).Put(
				[]byte(strings.Repeat("p", (bolt.MaxKeySize-2)/2+1)), encoded,
			)
		}},
		{name: "deletion provenance", mutate: func(transaction *bolt.Tx) error {
			encoded, _ := encodeRow("run", runRecord{
				OrderIdentity: []byte("identity"), SeedManifestDeleting: true,
			})
			return transaction.Bucket(runsBucket).Put(
				[]byte(strings.Repeat("p", (bolt.MaxKeySize-2)/2+1)), encoded,
			)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
			mutateCheckpoint(t, checkpoint, testCase.mutate)
			if err := checkpoint.resumeSeedManifestTransitions(
				testContext,
			); !errors.Is(
				err,
				ErrCorruptCheckpoint,
			) {
				t.Fatalf("seed transition discovery error = %v", err)
			}
		})
	}
}

type seedManifestDiscardFault struct {
	name   string
	mutate func(*bolt.Tx, seedManifestPublication, *runRecord) error
	done   bool
	want   error
}

var seedManifestDiscardFaults = []seedManifestDiscardFault{
	{
		name: "missing run",
		mutate: func(transaction *bolt.Tx, publication seedManifestPublication, _ *runRecord) error {
			return transaction.Bucket(runsBucket).Delete(publication.provenance)
		},
		done: true,
	},
	{
		name: "missing marker",
		mutate: func(_ *bolt.Tx, _ seedManifestPublication, record *runRecord) error {
			record.SeedManifestPublishing = false
			return nil
		},
		want: ErrCorruptCheckpoint,
	},
	{
		name: "missing bucket",
		mutate: func(transaction *bolt.Tx, _ seedManifestPublication, _ *runRecord) error {
			return transaction.DeleteBucket(seedManifestBucket)
		},
		want: ErrCorruptCheckpoint,
	},
}

func TestDiscardSeedManifestPublicationChunkHandlesMissingAndInvalidMarkers(t *testing.T) {
	for _, testCase := range seedManifestDiscardFaults {
		t.Run(testCase.name, func(t *testing.T) {
			runSeedManifestDiscardFault(t, testCase)
		})
	}
}

func runSeedManifestDiscardFault(t *testing.T, testCase seedManifestDiscardFault) {
	t.Helper()
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	page := testPage("https://discard.example/page", "discard.example", "discard", 0)
	encoded, _ := encodeSeedManifest([]Page{page})
	publication := testSeedManifestPublication(
		t,
		[]byte("discard-"+testCase.name),
		[]byte("identity"),
		encoded,
	)
	if _, err := checkpoint.prepareSeedManifestPublication(
		testContext,
		publication,
	); err != nil {
		t.Fatalf("prepare discard fixture: %v", err)
	}
	mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
		record, _, err := readRunRecord(transaction, publication.provenance)
		if err != nil {
			return err
		}
		if err := testCase.mutate(transaction, publication, &record); err != nil {
			return err
		}
		if transaction.Bucket(runsBucket).Get(publication.provenance) == nil {
			return nil
		}
		return writeRunRecord(transaction, publication.provenance, record)
	})
	done, err := checkpoint.discardSeedManifestPublicationChunk(
		testContext, publication.provenance, publication.prefix,
	)
	if done != testCase.done || !errors.Is(err, testCase.want) {
		t.Fatalf("discard chunk = %v, %v", done, err)
	}
}
