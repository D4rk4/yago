package frontiercheckpoint

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type largeDeletionFixture struct {
	path       string
	provenance []byte
	identity   []byte
	prefix     []byte
	pages      []Page
}

func TestLargeDeletionResumesAndAllowsProvenanceReuse(t *testing.T) {
	fixture := stagePartialLargeDeletion(t)
	requireRecoveredLargeDeletion(t, fixture)
}

func stagePartialLargeDeletion(t *testing.T) largeDeletionFixture {
	t.Helper()
	fixture := largeDeletionFixture{
		path:       testCheckpointPath(t),
		provenance: []byte("large-run"),
		identity:   []byte("large-identity"),
	}
	checkpoint := openTestCheckpoint(t, fixture.path)
	beginTestRun(t, checkpoint, fixture.provenance, fixture.identity)
	pages := largeDeletionPages(deletionRowsPerTransaction*3 + 17)
	if admitted, err := checkpoint.Admit(
		testContext,
		fixture.provenance,
		pages,
	); err != nil || admitted != len(pages) {
		t.Fatalf("admit large run = %d, %v, want %d", admitted, err, len(pages))
	}
	prefix, err := provenancePrefix(fixture.provenance)
	if err != nil {
		t.Fatalf("large run prefix: %v", err)
	}
	requireMultiLeafRun(t, checkpoint)
	rowsBefore := prefixedRowTotal(t, checkpoint, prefix)
	found, err := checkpoint.markRunDeleting(testContext, fixture.provenance)
	if err != nil || !found {
		t.Fatalf("mark large run deleting = %v, %v", found, err)
	}
	found, err = checkpoint.markRunDeleting(testContext, fixture.provenance)
	if err != nil || !found {
		t.Fatalf("repeat large run deletion marker = %v, %v", found, err)
	}
	requireDeletingRunSurfaces(t, checkpoint, fixture)
	done, err := checkpoint.deleteMarkedRunChunk(testContext, fixture.provenance, prefix)
	if err != nil || done {
		t.Fatalf("first deletion chunk = %v, %v", done, err)
	}
	rowsAfterChunk := prefixedRowTotal(t, checkpoint, prefix)
	if rowsBefore-rowsAfterChunk != deletionRowsPerTransaction {
		t.Fatalf(
			"first deletion removed %d rows, want %d",
			rowsBefore-rowsAfterChunk,
			deletionRowsPerTransaction,
		)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close partial deletion: %v", err)
	}
	fixture.prefix = prefix
	fixture.pages = pages

	return fixture
}

func requireDeletingRunSurfaces(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	fixture largeDeletionFixture,
) {
	t.Helper()
	if _, err := checkpoint.Status(
		testContext,
		fixture.provenance,
		fixture.identity,
	); !errors.Is(err, ErrRunDeleting) {
		t.Fatalf("deleting status error = %v", err)
	}
	if _, err := checkpoint.Inspect(
		testContext,
		fixture.provenance,
		fixture.identity,
	); !errors.Is(err, ErrRunDeleting) {
		t.Fatalf("deleting inspect error = %v", err)
	}
	if _, err := checkpoint.Load(testContext, fixture.provenance); !errors.Is(err, ErrRunDeleting) {
		t.Fatalf("deleting load error = %v", err)
	}
	if err := checkpoint.Begin(
		testContext,
		fixture.provenance,
		fixture.identity,
		yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
	); !errors.Is(err, ErrRunDeleting) {
		t.Fatalf("deleting begin error = %v", err)
	}
}

func requireRecoveredLargeDeletion(t *testing.T, fixture largeDeletionFixture) {
	t.Helper()
	reopened := openTestCheckpoint(t, fixture.path)
	status, err := reopened.Status(testContext, fixture.provenance, fixture.identity)
	if err != nil || status != RunMissing {
		t.Fatalf("resumed deletion status = %v, %v", status, err)
	}
	if rows := prefixedRowTotal(t, reopened, fixture.prefix); rows != 0 {
		t.Fatalf("rows after resumed deletion = %d, want 0", rows)
	}
	done, err := reopened.deleteMarkedRunChunk(
		testContext,
		fixture.provenance,
		fixture.prefix,
	)
	if err != nil || !done {
		t.Fatalf("missing deletion chunk = %v, %v", done, err)
	}
	replacementIdentity := []byte("replacement-identity")
	beginTestRun(t, reopened, fixture.provenance, replacementIdentity)
	if admitted, err := reopened.Admit(
		testContext,
		fixture.provenance,
		[]Page{fixture.pages[0]},
	); err != nil || admitted != 1 {
		t.Fatalf("reuse provenance admission = %d, %v", admitted, err)
	}
	snapshot, err := reopened.Load(testContext, fixture.provenance)
	if err != nil || len(snapshot.Outstanding) != 1 {
		t.Fatalf("replacement snapshot = %+v, %v", snapshot, err)
	}
	requirePageEqual(t, snapshot.Outstanding[0], fixture.pages[0])
}

func TestDeletionChunkRequiresPersistentMarker(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("unmarked-run")
	beginTestRun(t, checkpoint, provenance, []byte("unmarked-identity"))
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		t.Fatalf("unmarked prefix: %v", err)
	}
	if _, err := checkpoint.deleteMarkedRunChunk(
		testContext,
		provenance,
		prefix,
	); !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("unmarked deletion error = %v", err)
	}
}

func TestDeletionRecoveryPropagatesCorruption(t *testing.T) {
	t.Run("run record", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		provenance := []byte("corrupt-deletion-run")
		beginTestRun(t, checkpoint, provenance, []byte("corrupt-deletion-identity"))
		prefix, err := provenancePrefix(provenance)
		if err != nil {
			t.Fatalf("corrupt deletion prefix: %v", err)
		}
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return transaction.Bucket(runsBucket).Put(provenance, []byte("{"))
		})
		if _, err := checkpoint.deleteMarkedRunChunk(
			testContext,
			provenance,
			prefix,
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("corrupt deletion chunk error = %v", err)
		}
	})
	t.Run("run bucket", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		deleteSchemaBucket(t, checkpoint, runsBucket)
		if _, err := checkpoint.deletingProvenances(
			testContext,
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("missing deletion run bucket error = %v", err)
		}
	})
	t.Run("resume rows", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		provenance := []byte("missing-deletion-rows")
		beginTestRun(t, checkpoint, provenance, []byte("missing-deletion-identity"))
		found, err := checkpoint.markRunDeleting(testContext, provenance)
		if err != nil || !found {
			t.Fatalf("mark missing rows deletion = %v, %v", found, err)
		}
		deleteSchemaBucket(t, checkpoint, visitedBucket)
		if err := checkpoint.resumeDeletions(
			testContext,
		); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("resume missing deletion rows error = %v", err)
		}
	})
}

func TestOpenRejectsMalformedDeletionMarkersAndReleasesLock(t *testing.T) {
	cases := []struct {
		name       string
		provenance []byte
		record     []byte
	}{
		{name: "encoding", provenance: []byte("corrupt"), record: []byte("{")},
		{
			name:       "identity",
			provenance: []byte("identity"),
			record:     encodedRunRecord(t, runRecord{Deleting: true}),
		},
		{
			name:       "provenance",
			provenance: bytes.Repeat([]byte("p"), (bolt.MaxKeySize-2)/2+1),
			record: encodedRunRecord(t, runRecord{
				OrderIdentity: []byte("identity"),
				Deleting:      true,
			}),
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := testCheckpointPath(t)
			writeRawCheckpoint(t, path, func(transaction *bolt.Tx) error {
				if err := initializeSchema(transaction); err != nil {
					return err
				}
				return transaction.Bucket(runsBucket).Put(
					testCase.provenance,
					testCase.record,
				)
			})
			checkpoint, err := Open(path)
			if checkpoint != nil {
				_ = checkpoint.Close()
			}
			if !errors.Is(err, ErrCorruptCheckpoint) {
				t.Fatalf("open malformed deletion error = %v", err)
			}
			database, err := bolt.Open(path, 0o600, nil)
			if err != nil {
				t.Fatalf("reopen raw database after failed open: %v", err)
			}
			if err := database.Close(); err != nil {
				t.Fatalf("close raw database: %v", err)
			}
		})
	}
}

func largeDeletionPages(total int) []Page {
	pages := make([]Page, 0, total)
	for index := 0; index < total; index++ {
		host := fmt.Sprintf("host-%04d.example", index)
		pages = append(pages, testPage(
			fmt.Sprintf(
				"https://%s/%04d/%s",
				host,
				index,
				strings.Repeat("x", 64),
			),
			host,
			fmt.Sprintf("observation-%04d", index),
			index,
		))
	}
	return pages
}

func requireMultiLeafRun(t *testing.T, checkpoint *FrontierCheckpoint) {
	t.Helper()
	if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
		for _, name := range [][]byte{
			visitedBucket,
			pagesBucket,
			pagePositionsBucket,
			hostsBucket,
		} {
			if leaves := transaction.Bucket(name).Stats().LeafPageN; leaves < 2 {
				t.Fatalf("bucket %q leaf pages = %d, want at least 2", name, leaves)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect bucket leaves: %v", err)
	}
}

func prefixedRowTotal(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	prefix []byte,
) int {
	t.Helper()
	total := 0
	if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
		for _, name := range [][]byte{
			visitedBucket,
			pagesBucket,
			pagePositionsBucket,
			hostsBucket,
		} {
			cursor := transaction.Bucket(name).Cursor()
			for key, _ := cursor.Seek(prefix); key != nil && bytes.HasPrefix(
				key,
				prefix,
			); key, _ = cursor.Next() {
				total++
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("count prefixed rows: %v", err)
	}
	return total
}

func encodedRunRecord(t *testing.T, record runRecord) []byte {
	t.Helper()
	encoded, err := encodeRow("run", record)
	if err != nil {
		t.Fatalf("encode run record: %v", err)
	}
	return encoded
}
