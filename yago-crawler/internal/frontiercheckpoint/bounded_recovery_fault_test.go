package frontiercheckpoint

import (
	"errors"
	"math"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestBoundedRecoveryRejectsInvalidRequestsAndMissingState(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	for name, run := range map[string]func() error{
		"load provenance": func() error {
			_, err := checkpoint.LoadBounded(testContext, nil, 1)
			return err
		},
		"load zero limit": func() error {
			_, err := checkpoint.LoadBounded(testContext, []byte("run"), 0)
			return err
		},
		"load excessive limit": func() error {
			_, err := checkpoint.LoadBounded(testContext, []byte("run"), RecoveryPageBatchSize+1)
			return err
		},
		"load missing run": func() error {
			_, err := checkpoint.LoadBounded(testContext, []byte("missing"), 1)
			return err
		},
		"batch provenance": func() error {
			_, err := checkpoint.LoadRecoveryPageBatch(testContext, nil, 0, 0, 1)
			return err
		},
		"batch zero limit": func() error {
			_, err := checkpoint.LoadRecoveryPageBatch(testContext, []byte("run"), 0, 0, 0)
			return err
		},
		"batch excessive limit": func() error {
			_, err := checkpoint.LoadRecoveryPageBatch(testContext, []byte("run"), 0, 0, RecoveryPageBatchSize+1)
			return err
		},
		"batch inverted boundary": func() error {
			_, err := checkpoint.LoadRecoveryPageBatch(testContext, []byte("run"), 2, 1, 1)
			return err
		},
		"batch missing run": func() error {
			_, err := checkpoint.LoadRecoveryPageBatch(testContext, []byte("missing"), 0, 0, 1)
			return err
		},
		"seed provenance": func() error {
			_, _, _, err := checkpoint.LoadSeedPageBatch(testContext, nil, 0, 1)
			return err
		},
		"seed zero limit": func() error {
			_, _, _, err := checkpoint.LoadSeedPageBatch(testContext, []byte("run"), 0, 0)
			return err
		},
		"seed excessive limit": func() error {
			_, _, _, err := checkpoint.LoadSeedPageBatch(testContext, []byte("run"), 0, SeedAdmissionBatchSize+1)
			return err
		},
		"seed missing run": func() error {
			_, _, _, err := checkpoint.LoadSeedPageBatch(testContext, []byte("missing"), 0, 1)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := run(); err == nil {
				t.Fatal("invalid recovery request succeeded")
			}
		})
	}
}

type boundedRunInvariantFault struct {
	name   string
	mutate func(*bolt.Tx, []byte, []byte, *runRecord) error
}

var boundedRunInvariantFaults = []boundedRunInvariantFault{
	{
		name: "pages below pending",
		mutate: func(_ *bolt.Tx, _, _ []byte, record *runRecord) error {
			record.Pages = 0
			return nil
		},
	},
	{name: "completion marker", mutate: func(_ *bolt.Tx, _, _ []byte, record *runRecord) error {
		record.Completed = true
		return nil
	}},
	{
		name: "cursor after length",
		mutate: func(_ *bolt.Tx, _, _ []byte, record *runRecord) error {
			record.SeedManifest = true
			record.SeedCursor = 2
			record.SeedLength = 1
			return nil
		},
	},
	{
		name: "metadata without manifest",
		mutate: func(_ *bolt.Tx, _, _ []byte, record *runRecord) error {
			record.SeedCursor = 1
			return nil
		},
	},
	{
		name: "manifest after seeding",
		mutate: func(_ *bolt.Tx, _, _ []byte, record *runRecord) error {
			record.Seeding = false
			record.SeedManifest = true
			record.SeedLength = 1
			return nil
		},
	},
	{
		name: "empty manifest rows",
		mutate: func(transaction *bolt.Tx, prefix, _ []byte, record *runRecord) error {
			record.SeedManifest = true
			encoded, err := encodeRow(
				"seed",
				testPage("https://extra.example/", "extra.example", "extra", 0),
			)
			if err != nil {
				return err
			}
			return transaction.Bucket(seedManifestBucket).
				Put(sequenceRowKey(prefix, 1), encoded)
		},
	},
	{
		name: "missing first manifest row",
		mutate: func(_ *bolt.Tx, _, _ []byte, record *runRecord) error {
			record.SeedManifest = true
			record.SeedLength = 1
			return nil
		},
	},
	{
		name: "missing final manifest row",
		mutate: func(transaction *bolt.Tx, prefix, _ []byte, record *runRecord) error {
			record.SeedManifest = true
			record.SeedLength = 2
			encoded, err := encodeRow(
				"seed",
				testPage("https://first.example/", "first.example", "first", 0),
			)
			if err != nil {
				return err
			}
			return transaction.Bucket(seedManifestBucket).
				Put(sequenceRowKey(prefix, 1), encoded)
		},
	},
}

func TestBoundedLoadValidatesRunAndManifestInvariants(t *testing.T) {
	for _, testCase := range boundedRunInvariantFaults {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint, provenance, _ := admittedCheckpoint(t)
			prefix, _ := provenancePrefix(provenance)
			mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
				record, _, err := readRunRecord(transaction, provenance)
				if err != nil {
					return err
				}
				if err := testCase.mutate(transaction, prefix, provenance, &record); err != nil {
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
				t.Fatalf("bounded invariant error = %v", err)
			}
		})
	}
}

type boundedRecoveryRelationshipFault struct {
	name   string
	mutate func(*bolt.Tx, []byte, Page) error
}

var boundedRecoveryRelationshipFaults = []boundedRecoveryRelationshipFault{
	{name: "page key", mutate: func(transaction *bolt.Tx, prefix []byte, _ Page) error {
		if err := transaction.Bucket(pagesBucket).
			Delete(sequenceRowKey(prefix, 1)); err != nil {
			return wrapDatabaseError("delete corrupt page fixture", err)
		}
		return transaction.Bucket(pagesBucket).Put(childRowKey(prefix, "bad"), []byte("{}"))
	}},
	{name: "page encoding", mutate: func(transaction *bolt.Tx, prefix []byte, _ Page) error {
		return transaction.Bucket(pagesBucket).Put(sequenceRowKey(prefix, 1), []byte("{"))
	}},
	{name: "page fields", mutate: func(transaction *bolt.Tx, prefix []byte, page Page) error {
		page.URL = ""
		encoded, _ := encodeRow("page", page)
		return transaction.Bucket(pagesBucket).Put(sequenceRowKey(prefix, 1), encoded)
	}},
	{
		name: "position width",
		mutate: func(transaction *bolt.Tx, prefix []byte, page Page) error {
			return transaction.Bucket(pagePositionsBucket).
				Put(childRowKey(prefix, page.URL), []byte{1})
		},
	},
	{
		name: "position value",
		mutate: func(transaction *bolt.Tx, prefix []byte, page Page) error {
			return transaction.Bucket(pagePositionsBucket).
				Put(childRowKey(prefix, page.URL), sequenceValue(2))
		},
	},
	{name: "visited", mutate: func(transaction *bolt.Tx, prefix []byte, page Page) error {
		return transaction.Bucket(visitedBucket).Delete(childRowKey(prefix, page.URL))
	}},
	{name: "host encoding", mutate: func(transaction *bolt.Tx, prefix []byte, page Page) error {
		return transaction.Bucket(hostsBucket).Put(childRowKey(prefix, page.Host), []byte("{"))
	}},
	{
		name: "redirect target absent",
		mutate: func(transaction *bolt.Tx, prefix []byte, page Page) error {
			page.RedirectURL = "https://redirect.example/final"
			page.RedirectHost = "redirect.example"
			page.RedirectHostBump = true
			encoded, _ := encodeRow("page", page)
			return transaction.Bucket(pagesBucket).Put(sequenceRowKey(prefix, 1), encoded)
		},
	},
	{
		name: "redirect ownership without target",
		mutate: func(transaction *bolt.Tx, prefix []byte, page Page) error {
			page.RedirectHost = "redirect.example"
			encoded, _ := encodeRow("page", page)
			return transaction.Bucket(pagesBucket).Put(sequenceRowKey(prefix, 1), encoded)
		},
	},
	{
		name: "redirect source equals target",
		mutate: func(transaction *bolt.Tx, prefix []byte, page Page) error {
			page.RedirectURL = page.URL
			page.RedirectHost = page.Host
			if err := transaction.Bucket(visitedBucket).
				Put(childRowKey(prefix, page.RedirectURL), visitedMarker); err != nil {
				return wrapDatabaseError("write redirect target fixture", err)
			}
			encoded, _ := encodeRow("page", page)
			return transaction.Bucket(pagesBucket).Put(sequenceRowKey(prefix, 1), encoded)
		},
	},
	{
		name: "redirect host absent",
		mutate: func(transaction *bolt.Tx, prefix []byte, page Page) error {
			page.RedirectURL = "https://redirect.example/final"
			if err := transaction.Bucket(visitedBucket).
				Put(childRowKey(prefix, page.RedirectURL), visitedMarker); err != nil {
				return wrapDatabaseError("write redirect target fixture", err)
			}
			encoded, _ := encodeRow("page", page)
			return transaction.Bucket(pagesBucket).Put(sequenceRowKey(prefix, 1), encoded)
		},
	},
	{
		name: "redirect bump mismatch",
		mutate: func(transaction *bolt.Tx, prefix []byte, page Page) error {
			page.RedirectURL = "https://redirect.example/final"
			page.RedirectHost = "redirect.example"
			if err := transaction.Bucket(visitedBucket).
				Put(childRowKey(prefix, page.RedirectURL), visitedMarker); err != nil {
				return wrapDatabaseError("write redirect target fixture", err)
			}
			encoded, _ := encodeRow("page", page)
			return transaction.Bucket(pagesBucket).Put(sequenceRowKey(prefix, 1), encoded)
		},
	},
	{
		name: "redirect target outstanding",
		mutate: func(transaction *bolt.Tx, prefix []byte, page Page) error {
			page.RedirectURL = "https://redirect.example/final"
			page.RedirectHost = "redirect.example"
			page.RedirectHostBump = true
			if err := transaction.Bucket(visitedBucket).
				Put(childRowKey(prefix, page.RedirectURL), visitedMarker); err != nil {
				return wrapDatabaseError("write redirect target fixture", err)
			}
			if err := transaction.Bucket(pagePositionsBucket).
				Put(childRowKey(prefix, page.RedirectURL), sequenceValue(2)); err != nil {
				return wrapDatabaseError("write redirect position fixture", err)
			}
			encoded, _ := encodeRow("page", page)
			return transaction.Bucket(pagesBucket).Put(sequenceRowKey(prefix, 1), encoded)
		},
	},
}

func TestBoundedLoadRejectsCorruptRecoveryRelationships(t *testing.T) {
	for _, testCase := range boundedRecoveryRelationshipFaults {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint, provenance, page := admittedCheckpoint(t)
			prefix, _ := provenancePrefix(provenance)
			mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
				return testCase.mutate(transaction, prefix, page)
			})
			if _, err := checkpoint.LoadBounded(
				testContext,
				provenance,
				1,
			); !errors.Is(
				err,
				ErrCorruptCheckpoint,
			) {
				t.Fatalf("bounded relationship error = %v", err)
			}
		})
	}
}

func TestRecoveryBatchBoundariesAndCancellationRows(t *testing.T) {
	checkpoint, provenance, page := admittedCheckpoint(t)
	batch, err := checkpoint.LoadRecoveryPageBatch(testContext, provenance, 1, 1, 1)
	if err != nil || !batch.Complete || batch.Cursor != 1 || len(batch.Pages) != 0 {
		t.Fatalf("empty recovery batch = %+v, %v", batch, err)
	}
	if _, err := checkpoint.LoadRecoveryPageBatch(
		testContext,
		provenance,
		0,
		2,
		1,
	); !errors.Is(
		err,
		ErrCorruptCheckpoint,
	) {
		t.Fatalf("excess recovery boundary error = %v", err)
	}
	if err := checkpoint.UpdateControl(
		testContext,
		provenance,
		ControlUpdate{Cancelled: true},
	); err != nil {
		t.Fatalf("cancel recovery fixture: %v", err)
	}
	prefix, _ := provenancePrefix(provenance)
	mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
		if err := transaction.Bucket(pagesBucket).Delete(sequenceRowKey(prefix, 1)); err != nil {
			return wrapDatabaseError("delete cancellation page fixture", err)
		}
		return transaction.Bucket(pagesBucket).Put(childRowKey(prefix, page.URL), []byte("{}"))
	})
	if _, err := checkpoint.CancelRecoveryPages(
		testContext,
		provenance,
		0,
		1,
	); !errors.Is(
		err,
		ErrCorruptCheckpoint,
	) {
		t.Fatalf("corrupt cancellation page error = %v", err)
	}
}

func TestRecoveryHelpersRejectOverflowAndInvalidRows(t *testing.T) {
	checkpoint, provenance, page := admittedCheckpoint(t)
	prefix, _ := provenancePrefix(provenance)
	err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
		buckets, err := loadCheckpointBuckets(transaction)
		if err != nil {
			return err
		}
		if _, err := readRecoveryPageBatch(recoveryPageRead{
			buckets: buckets, prefix: prefix, after: math.MaxUint64, upper: 0, limit: 1,
		}); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("recovery overflow error = %v", err)
		}
		if _, _, _, err := recoveryPageURLs(
			buckets.pages, prefix, math.MaxUint64, math.MaxUint64, 1,
		); err != nil {
			t.Fatalf("equal maximum recovery cursor: %v", err)
		}
		if _, _, _, err := recoveryPageURLs(
			buckets.pages, prefix, math.MaxUint64, 0, 1,
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("recovery URL overflow error = %v", err)
		}
		if retiredPageTotalMatches(0, []string{page.URL}) || !retiredPageTotalMatches(0, nil) {
			t.Fatal("retired page accounting accepted a mismatched total")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect recovery helpers: %v", err)
	}
}

func TestSeedBatchReadsRejectCursorStateAndExcessRows(t *testing.T) {
	page := testPage("https://seed-read.example/page", "seed-read.example", "seed-read", 0)
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("seed-read-state")
	beginSeedManifest(t, checkpoint, provenance, []Page{page})
	if _, _, _, err := checkpoint.LoadSeedPageBatch(
		testContext,
		provenance,
		1,
		1,
	); !errors.Is(
		err,
		ErrInvalidSeedBatch,
	) {
		t.Fatalf("seed cursor state error = %v", err)
	}
	prefix, _ := provenancePrefix(provenance)
	mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
		encoded, err := encodeRow("seed", page)
		if err != nil {
			return err
		}
		return transaction.Bucket(seedManifestBucket).Put(sequenceRowKey(prefix, 2), encoded)
	})
	if _, _, _, err := checkpoint.LoadSeedPageBatch(
		testContext,
		provenance,
		0,
		1,
	); !errors.Is(
		err,
		ErrCorruptCheckpoint,
	) {
		t.Fatalf("excess seed batch error = %v", err)
	}
}

func TestAdmissionBatchStateRejectsCorruptVisitedAndHostRows(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*bolt.Tx, []byte, Page) error
	}{
		{name: "visited marker", mutate: func(transaction *bolt.Tx, prefix []byte, page Page) error {
			return transaction.Bucket(visitedBucket).Put(childRowKey(prefix, page.URL), []byte{2})
		}},
		{name: "host row", mutate: func(transaction *bolt.Tx, prefix []byte, page Page) error {
			return transaction.Bucket(hostsBucket).Put(childRowKey(prefix, page.Host), []byte("{"))
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint, provenance, page := admittedCheckpoint(t)
			prefix, _ := provenancePrefix(provenance)
			mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
				return testCase.mutate(transaction, prefix, page)
			})
			if _, err := checkpoint.AdmissionBatchState(
				testContext,
				provenance,
				[]Page{page},
			); !errors.Is(
				err,
				ErrCorruptCheckpoint,
			) {
				t.Fatalf("corrupt admission state error = %v", err)
			}
		})
	}
}
