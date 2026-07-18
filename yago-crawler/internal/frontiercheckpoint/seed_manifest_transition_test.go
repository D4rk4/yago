package frontiercheckpoint

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type seedManifestCrashFixture struct {
	path                  string
	provenance            []byte
	identity              []byte
	pages                 []Page
	interleavedProvenance []byte
	interleavedIdentity   []byte
}

func largeSeedManifestPages(total int) []Page {
	pages := make([]Page, total)
	for index := range pages {
		pages[index] = testPage(
			fmt.Sprintf("https://manifest.example/%05d", index),
			"manifest.example",
			fmt.Sprintf("manifest-%05d", index),
			0,
		)
	}

	return pages
}

func seedManifestRowTotal(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	provenance []byte,
) int {
	t.Helper()
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		t.Fatalf("manifest row prefix: %v", err)
	}
	total := 0
	if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
		cursor := transaction.Bucket(seedManifestBucket).Cursor()
		for key, _ := cursor.Seek(prefix); key != nil && bytes.HasPrefix(key, prefix); key, _ = cursor.Next() {
			total++
		}

		return nil
	}); err != nil {
		t.Fatalf("count manifest rows: %v", err)
	}

	return total
}

func testSeedManifestPublication(
	t *testing.T,
	provenance []byte,
	orderIdentity []byte,
	encodedPages [][]byte,
) seedManifestPublication {
	t.Helper()
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		t.Fatalf("derive test manifest prefix: %v", err)
	}
	manifestLength := seedManifestPageTotal(encodedPages)

	return seedManifestPublication{
		provenance:       provenance,
		prefix:           prefix,
		orderIdentity:    orderIdentity,
		priority:         yagocrawlcontract.CrawlOrderPriorityNormal,
		encodedPages:     encodedPages,
		manifestIdentity: identifySeedManifest(encodedPages),
		manifestLength:   manifestLength,
	}
}

func TestSeedManifestPublicationCrashDiscardsIncompleteRowsAndKeepsInterleavedRun(
	t *testing.T,
) {
	path := testCheckpointPath(t)
	checkpoint := openTestCheckpoint(t, path)
	provenance := []byte("manifest-publication-crash")
	identity := []byte("manifest-publication-order")
	pages := largeSeedManifestPages(seedManifestRowsPerTransaction*2 + 1)
	encodedPages, err := encodeSeedManifest(pages)
	if err != nil {
		t.Fatalf("encode large seed manifest: %v", err)
	}
	publication := testSeedManifestPublication(t, provenance, identity, encodedPages)
	publishing, err := checkpoint.prepareSeedManifestPublication(
		testContext,
		publication,
	)
	if err != nil || !publishing {
		t.Fatalf("prepare large seed manifest: publishing=%v err=%v", publishing, err)
	}
	done, err := checkpoint.stageSeedManifestChunk(
		testContext,
		publication,
	)
	if err != nil || done {
		t.Fatalf("stage first manifest chunk: done=%v err=%v", done, err)
	}
	if total := seedManifestRowTotal(
		t,
		checkpoint,
		provenance,
	); total != seedManifestRowsPerTransaction {
		t.Fatalf("first manifest transaction rows = %d", total)
	}
	interleavedProvenance := []byte("manifest-interleaved-run")
	interleavedIdentity := []byte("manifest-interleaved-order")
	if err := checkpoint.Begin(
		testContext,
		interleavedProvenance,
		interleavedIdentity,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("interleave run write between manifest chunks: %v", err)
	}
	changedPages := append([]Page(nil), pages...)
	changedPages[0].ObservationID = "changed"
	if err := checkpoint.BeginSeedManifest(
		testContext,
		provenance,
		identity,
		yagocrawlcontract.CrawlOrderPriorityNormal,
		changedPages,
	); !errors.Is(err, ErrProvenanceCollision) {
		t.Fatalf("changed staged manifest error = %v", err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close after partial manifest publication: %v", err)
	}
	requireSeedManifestCrashRecovery(t, seedManifestCrashFixture{
		path:                  path,
		provenance:            provenance,
		identity:              identity,
		pages:                 pages,
		interleavedProvenance: interleavedProvenance,
		interleavedIdentity:   interleavedIdentity,
	})
}

func requireSeedManifestCrashRecovery(t *testing.T, fixture seedManifestCrashFixture) {
	t.Helper()
	checkpoint := openTestCheckpoint(t, fixture.path)
	status, err := checkpoint.Status(testContext, fixture.provenance, fixture.identity)
	if err != nil || status != RunMissing {
		t.Fatalf("discarded manifest publication status = %v, %v", status, err)
	}
	if total := seedManifestRowTotal(t, checkpoint, fixture.provenance); total != 0 {
		t.Fatalf("discarded manifest rows = %d", total)
	}
	status, err = checkpoint.Status(
		testContext,
		fixture.interleavedProvenance,
		fixture.interleavedIdentity,
	)
	if err != nil || status != RunActive {
		t.Fatalf("interleaved run status = %v, %v", status, err)
	}
	if err := checkpoint.BeginSeedManifest(
		testContext,
		fixture.provenance,
		fixture.identity,
		yagocrawlcontract.CrawlOrderPriorityNormal,
		fixture.pages,
	); err != nil {
		t.Fatalf("publish manifest after recovery: %v", err)
	}
	snapshot, err := checkpoint.Load(testContext, fixture.provenance)
	if err != nil || !snapshot.SeedManifest || len(snapshot.SeedPages) != len(fixture.pages) {
		t.Fatalf(
			"recovered publication snapshot pages=%d manifest=%v err=%v",
			len(snapshot.SeedPages),
			snapshot.SeedManifest,
			err,
		)
	}
}

func TestMaximumSeedManifestPublicationIsChunkedAndInvisibleUntilComplete(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("maximum-seed-manifest")
	identity := []byte("maximum-seed-manifest-order")
	pages := largeSeedManifestPages(50_000)
	encodedPages, err := encodeSeedManifest(pages)
	if err != nil {
		t.Fatalf("encode maximum seed manifest: %v", err)
	}
	publication := testSeedManifestPublication(t, provenance, identity, encodedPages)
	publishing, err := checkpoint.prepareSeedManifestPublication(
		testContext,
		publication,
	)
	if err != nil || !publishing {
		t.Fatalf("prepare maximum seed manifest: publishing=%v err=%v", publishing, err)
	}
	if _, err := checkpoint.AdmitSeedBatch(testContext, provenance, SeedBatch{
		Decisions: []SeedDecision{{Page: pages[0]}},
	}); !errors.Is(err, ErrSeedManifestMissing) {
		t.Fatalf("unpublished manifest admission error = %v", err)
	}
	chunks := 0
	for {
		done, err := checkpoint.stageSeedManifestChunk(
			testContext,
			publication,
		)
		if err != nil {
			t.Fatalf("stage maximum manifest chunk %d: %v", chunks, err)
		}
		chunks++
		if done {
			break
		}
	}
	wantChunks := (len(pages) + seedManifestRowsPerTransaction - 1) /
		seedManifestRowsPerTransaction
	if chunks != wantChunks {
		t.Fatalf("maximum manifest chunks = %d, want %d", chunks, wantChunks)
	}
	if _, err := checkpoint.AdmitSeedBatch(testContext, provenance, SeedBatch{
		Decisions: []SeedDecision{{Page: pages[0]}},
	}); !errors.Is(err, ErrSeedManifestMissing) {
		t.Fatalf("staged unpublished manifest admission error = %v", err)
	}
	if err := checkpoint.completeSeedManifestPublication(
		testContext,
		provenance,
		publication.manifestIdentity,
		publication.manifestLength,
	); err != nil {
		t.Fatalf("publish maximum seed manifest: %v", err)
	}
	snapshot, err := checkpoint.Load(testContext, provenance)
	if err != nil || !snapshot.SeedManifest || len(snapshot.SeedPages) != len(pages) {
		t.Fatalf(
			"maximum manifest snapshot pages=%d manifest=%v err=%v",
			len(snapshot.SeedPages),
			snapshot.SeedManifest,
			err,
		)
	}
}

func TestConsumedSeedManifestCleanupResumesAcrossCrashAndIsIdempotent(t *testing.T) {
	path := testCheckpointPath(t)
	checkpoint := openTestCheckpoint(t, path)
	provenance := []byte("manifest-cleanup-crash")
	pages := largeSeedManifestPages(seedManifestRowsPerTransaction*2 + 17)
	beginSeedManifest(t, checkpoint, provenance, pages)
	for cursor := 0; cursor < len(pages); cursor += SeedAdmissionBatchSize {
		end := min(cursor+SeedAdmissionBatchSize, len(pages))
		decisions := make([]SeedDecision, end-cursor)
		for index := range decisions {
			decisions[index] = SeedDecision{Page: pages[cursor+index]}
		}
		if _, err := checkpoint.AdmitSeedBatch(testContext, provenance, SeedBatch{
			Cursor:    uint64(cursor),
			Decisions: decisions,
		}); err != nil {
			t.Fatalf("advance seed manifest cursor %d: %v", cursor, err)
		}
	}
	prefix, _ := provenancePrefix(provenance)
	cleanup, err := checkpoint.prepareSeedingFinish(
		testContext,
		provenance,
		prefix,
		yagocrawlcontract.CrawlRunTally{Duplicates: 99},
	)
	if err != nil || !cleanup {
		t.Fatalf("prepare seed manifest cleanup: cleanup=%v err=%v", cleanup, err)
	}
	done, err := checkpoint.deleteConsumedSeedManifestChunk(testContext, provenance, prefix)
	if err != nil || done {
		t.Fatalf("delete first seed manifest chunk: done=%v err=%v", done, err)
	}
	if total := seedManifestRowTotal(
		t,
		checkpoint,
		provenance,
	); total != len(
		pages,
	)-seedManifestRowsPerTransaction {
		t.Fatalf("remaining manifest rows after first cleanup = %d", total)
	}
	interleavedProvenance := []byte("manifest-cleanup-interleaved")
	interleavedIdentity := []byte("manifest-cleanup-interleaved-order")
	if err := checkpoint.Begin(
		testContext,
		interleavedProvenance,
		interleavedIdentity,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("interleave run during manifest cleanup: %v", err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close during seed manifest cleanup: %v", err)
	}
	checkpoint = openTestCheckpoint(t, path)
	snapshot, err := checkpoint.Load(testContext, provenance)
	if err != nil || snapshot.Seeding || snapshot.SeedManifest || !snapshot.Completed ||
		len(snapshot.SeedPages) != 0 || snapshot.Tally != (yagocrawlcontract.CrawlRunTally{}) {
		t.Fatalf("resumed seed manifest cleanup snapshot = %+v, %v", snapshot, err)
	}
	if total := seedManifestRowTotal(t, checkpoint, provenance); total != 0 {
		t.Fatalf("resumed cleanup manifest rows = %d", total)
	}
	if err := checkpoint.FinishSeeding(
		testContext,
		provenance,
		yagocrawlcontract.CrawlRunTally{Duplicates: 99},
	); err != nil {
		t.Fatalf("repeat consumed manifest finish: %v", err)
	}
	snapshot, err = checkpoint.Load(testContext, provenance)
	if err != nil || snapshot.Tally != (yagocrawlcontract.CrawlRunTally{}) {
		t.Fatalf("idempotent consumed manifest tally = %+v, %v", snapshot.Tally, err)
	}
	status, err := checkpoint.Status(testContext, interleavedProvenance, interleavedIdentity)
	if err != nil || status != RunActive {
		t.Fatalf("cleanup interleaved run status = %v, %v", status, err)
	}
}
