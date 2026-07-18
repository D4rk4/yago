package frontiercheckpoint

import (
	"errors"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestBoundedRecoveryAcceptsEmptyManifestAndRedirectOwnership(t *testing.T) {
	t.Run("empty manifest", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		provenance := []byte("bounded-empty-manifest")
		beginSeedManifest(t, checkpoint, provenance, nil)
		snapshot, err := checkpoint.LoadBounded(testContext, provenance, 1)
		if err != nil || !snapshot.SeedManifest || snapshot.SeedLength != 0 ||
			!snapshot.RecoveryComplete {
			t.Fatalf("empty bounded manifest = %+v, %v", snapshot, err)
		}
	})
	t.Run("redirect", func(t *testing.T) {
		checkpoint, provenance, page := admittedCheckpoint(t)
		if recorded, err := checkpoint.RecordRedirect(
			testContext,
			provenance,
			testRedirect(page, "https://redirect.example/final", "redirect.example", true),
		); err != nil || !recorded {
			t.Fatalf("record bounded redirect = %v, %v", recorded, err)
		}
		snapshot, err := checkpoint.LoadBounded(testContext, provenance, 1)
		if err != nil || len(snapshot.Outstanding) != 1 ||
			snapshot.Outstanding[0].RedirectURL == "" {
			t.Fatalf("bounded redirect snapshot = %+v, %v", snapshot, err)
		}
	})
}

func TestBoundedRecoveryRejectsPendingAndSeedStorageMismatch(t *testing.T) {
	t.Run("pending", func(t *testing.T) {
		checkpoint, provenance, _ := admittedCheckpoint(t)
		mutateRunRecord(t, checkpoint, provenance, func(record *runRecord) {
			record.Pages = 2
			record.Pending = 2
		})
		if _, err := checkpoint.LoadBounded(
			testContext,
			provenance,
			1,
		); !errors.Is(
			err,
			ErrCorruptCheckpoint,
		) {
			t.Fatalf("bounded pending mismatch error = %v", err)
		}
	})
	t.Run("missing manifest bucket", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		provenance := []byte("bounded-missing-manifest")
		page := testPage("https://manifest.example/page", "manifest.example", "manifest", 0)
		beginSeedManifest(t, checkpoint, provenance, []Page{page})
		deleteSchemaBucket(t, checkpoint, seedManifestBucket)
		if _, _, _, err := checkpoint.LoadSeedPageBatch(
			testContext, provenance, 0, 1,
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("missing bounded manifest bucket error = %v", err)
		}
	})
	t.Run("missing manifest page", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		provenance := []byte("bounded-missing-page")
		page := testPage("https://manifest.example/page", "manifest.example", "manifest", 0)
		beginSeedManifest(t, checkpoint, provenance, []Page{page})
		prefix, _ := provenancePrefix(provenance)
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return transaction.Bucket(seedManifestBucket).Delete(sequenceRowKey(prefix, 1))
		})
		if _, _, _, err := checkpoint.LoadSeedPageBatch(
			testContext, provenance, 0, 1,
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("missing bounded manifest page error = %v", err)
		}
	})
}

func TestAdmissionBatchStateRejectsMissingFrontierBuckets(t *testing.T) {
	for _, bucket := range [][]byte{visitedBucket, hostsBucket} {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		provenance := []byte("admission-missing-" + string(bucket))
		beginTestRun(t, checkpoint, provenance, []byte("identity"))
		deleteSchemaBucket(t, checkpoint, bucket)
		page := testPage("https://state.example/page", "state.example", "state", 0)
		if _, err := checkpoint.AdmissionBatchState(
			testContext, provenance, []Page{page},
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("missing admission bucket %s error = %v", bucket, err)
		}
	}
}

func TestSnapshotRedirectAssociationRejectsEveryBrokenOwnershipShape(t *testing.T) {
	page := testPage("https://source.example/page", "source.example", "source", 0)
	visited := map[string]struct{}{page.URL: {}, "https://target.example/page": {}}
	for _, testCase := range []struct {
		name   string
		page   Page
		owners map[string]string
		setup  func(*testing.T, *bolt.Bucket, []byte)
	}{
		{name: "ownership without target", page: func() Page {
			changed := page
			changed.RedirectHost = "target.example"
			return changed
		}()},
		{name: "missing host", page: func() Page {
			changed := page
			changed.RedirectURL = "https://target.example/page"
			return changed
		}()},
		{name: "bump", page: func() Page {
			changed := page
			changed.RedirectURL = "https://target.example/page"
			changed.RedirectHost = "target.example"
			return changed
		}()},
		{name: "self", page: func() Page {
			changed := page
			changed.RedirectURL = page.URL
			changed.RedirectHost = page.Host
			return changed
		}()},
		{name: "unvisited", page: func() Page {
			changed := page
			changed.RedirectURL = "https://absent.example/page"
			changed.RedirectHost = "absent.example"
			changed.RedirectHostBump = true
			return changed
		}()},
		{name: "multiple owners", page: func() Page {
			changed := page
			changed.RedirectURL = "https://target.example/page"
			changed.RedirectHost = "target.example"
			changed.RedirectHostBump = true
			return changed
		}(), owners: map[string]string{"https://target.example/page": "https://other.example/page"}},
		{name: "outstanding target", page: func() Page {
			changed := page
			changed.RedirectURL = "https://target.example/page"
			changed.RedirectHost = "target.example"
			changed.RedirectHostBump = true
			return changed
		}(), setup: func(t *testing.T, positions *bolt.Bucket, prefix []byte) {
			if err := positions.Put(childRowKey(prefix, "https://target.example/page"), sequenceValue(2)); err != nil {
				t.Fatalf("write target position: %v", err)
			}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
			prefix := []byte{1, 2, 3}
			if testCase.setup != nil {
				mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
					testCase.setup(t, transaction.Bucket(pagePositionsBucket), prefix)
					return nil
				})
			}
			owners := testCase.owners
			if owners == nil {
				owners = make(map[string]string)
			}
			if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
				buckets, err := loadCheckpointBuckets(transaction)
				if err != nil {
					return err
				}
				return validateRedirectAssociation(buckets, prefix, testCase.page, visited, owners)
			}); !errors.Is(err, ErrCorruptCheckpoint) {
				t.Fatalf("redirect association error = %v", err)
			}
		})
	}
}

func TestSnapshotValidationRejectsSeedAndCompletionInvariants(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		snapshot Snapshot
	}{
		{name: "pending", snapshot: Snapshot{Counters: Counters{Pending: 1}}},
		{name: "pages", snapshot: Snapshot{Counters: Counters{Pending: 1}, Outstanding: []Page{{}}}},
		{name: "seed cursor", snapshot: Snapshot{Seeding: true, SeedManifest: true, SeedCursor: 2, SeedLength: 1, SeedPages: []Page{{}}}},
		{name: "seed length", snapshot: Snapshot{Seeding: true, SeedManifest: true, SeedLength: 1}},
		{name: "seed marker", snapshot: Snapshot{Seeding: true, SeedCursor: 1, SeedLength: 1, SeedPages: []Page{{}}}},
		{name: "manifest after seeding", snapshot: Snapshot{SeedManifest: true}},
		{name: "completion", snapshot: Snapshot{Seeding: true, Completed: true}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validateSnapshot(testCase.snapshot); !errors.Is(err, ErrCorruptCheckpoint) {
				t.Fatalf("snapshot invariant error = %v", err)
			}
		})
	}
}

func TestLoadSeedManifestRejectsMarkerlessMetadataAndCapacity(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
		prefix := []byte{1, 2}
		for _, record := range []runRecord{
			{SeedLength: 1},
			{SeedManifest: true, SeedCursor: 2, SeedLength: 1},
			{SeedManifest: true, SeedLength: uint64(^uint(0)>>1) + 1},
		} {
			if err := loadSeedManifest(
				transaction,
				prefix,
				record,
				&Snapshot{},
			); !errors.Is(
				err,
				ErrCorruptCheckpoint,
			) {
				t.Fatalf("seed manifest record %+v error = %v", record, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect seed manifest invariants: %v", err)
	}
}
