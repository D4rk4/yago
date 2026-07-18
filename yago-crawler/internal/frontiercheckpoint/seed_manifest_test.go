package frontiercheckpoint

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func beginSeedManifest(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	provenance []byte,
	pages []Page,
) []byte {
	t.Helper()
	identity := []byte("seed-manifest-order")
	if err := checkpoint.BeginSeedManifest(
		testContext,
		provenance,
		identity,
		yagocrawlcontract.CrawlOrderPriorityNormal,
		pages,
	); err != nil {
		t.Fatalf("begin seed manifest: %v", err)
	}
	return identity
}

func TestSeedManifestPrecedesAdmissionAndFinishesAtomically(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("manifest-lifecycle")
	first := testPage("https://example.com/first", "example.com", "first", 0)
	duplicate := testPage("https://example.com/first", "example.com", "duplicate", 0)
	skipped := testPage("https://example.com/skipped", "example.com", "skipped", 0)
	identity := beginSeedManifest(t, checkpoint, provenance, []Page{first, duplicate, skipped})
	snapshot, err := checkpoint.Load(testContext, provenance)
	if err != nil || !snapshot.SeedManifest || snapshot.SeedCursor != 0 ||
		snapshot.Counters != (Counters{}) || len(snapshot.Outstanding) != 0 ||
		len(snapshot.SeedPages) != 3 {
		t.Fatalf("manifest before admission = %+v, %v", snapshot, err)
	}
	for index, page := range []Page{first, duplicate, skipped} {
		requirePageEqual(t, snapshot.SeedPages[index], page)
	}
	result, err := checkpoint.AdmitSeedBatch(testContext, provenance, SeedBatch{
		Decisions: []SeedDecision{
			{Page: first, Admit: true},
			{Page: duplicate},
			{Page: skipped},
		},
	})
	if err != nil || result != (SeedBatchResult{Admitted: 1, Duplicates: 1}) {
		t.Fatalf("seed admission result = %+v, %v", result, err)
	}
	state, err := checkpoint.Inspect(testContext, provenance, identity)
	if err != nil || !state.SeedManifest || state.Pending != 1 ||
		state.Pages != 1 || state.Tally.Duplicates != 1 {
		t.Fatalf("seed admission state = %+v, %v", state, err)
	}
	if err := checkpoint.FinishSeeding(
		testContext,
		provenance,
		yagocrawlcontract.CrawlRunTally{Duplicates: 99},
	); err != nil {
		t.Fatalf("finish manifested seeding: %v", err)
	}
	snapshot, err = checkpoint.Load(testContext, provenance)
	if err != nil || snapshot.Seeding || snapshot.SeedManifest ||
		snapshot.SeedCursor != 0 || len(snapshot.SeedPages) != 0 ||
		snapshot.Tally.Duplicates != 1 {
		t.Fatalf("finished manifest snapshot = %+v, %v", snapshot, err)
	}
	prefix, _ := provenancePrefix(provenance)
	if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
		key, _ := transaction.Bucket(seedManifestBucket).Cursor().Seek(prefix)
		if key != nil && bytes.HasPrefix(key, prefix) {
			t.Fatalf("finished manifest retained row %q", key)
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect manifest cleanup: %v", err)
	}
	if err := checkpoint.CompletePage(
		testContext,
		provenance,
		first.URL,
		testPageCompletion(),
	); err != nil {
		t.Fatalf("complete manifested page: %v", err)
	}
}

func TestSeedManifestCursorAdvancesAcrossZeroPendingBatch(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("manifest-zero-pending")
	pages := make([]Page, SeedAdmissionBatchSize+1)
	for index := range pages {
		pages[index] = testPage(
			fmt.Sprintf("https://example.com/%03d", index),
			"example.com",
			fmt.Sprintf("observation-%03d", index),
			0,
		)
	}
	identity := beginSeedManifest(t, checkpoint, provenance, pages)
	decisions := make([]SeedDecision, SeedAdmissionBatchSize)
	for index := range decisions {
		decisions[index] = SeedDecision{Page: pages[index]}
	}
	result, err := checkpoint.AdmitSeedBatch(
		testContext,
		provenance,
		SeedBatch{Decisions: decisions},
	)
	if err != nil || result != (SeedBatchResult{}) {
		t.Fatalf("zero-pending seed batch = %+v, %v", result, err)
	}
	snapshot, err := checkpoint.Load(testContext, provenance)
	if err != nil || snapshot.SeedCursor != SeedAdmissionBatchSize ||
		snapshot.Counters.Pending != 0 || snapshot.Completed {
		t.Fatalf("zero-pending seed snapshot = %+v, %v", snapshot, err)
	}
	if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); !errors.Is(
		err,
		ErrInvalidSeedBatch,
	) {
		t.Fatalf("unfinished manifest finish error = %v", err)
	}
	result, err = checkpoint.AdmitSeedBatch(testContext, provenance, SeedBatch{
		Cursor: SeedAdmissionBatchSize,
		Decisions: []SeedDecision{{
			Page: pages[SeedAdmissionBatchSize],
		}},
	})
	if err != nil || result != (SeedBatchResult{}) {
		t.Fatalf("final skipped seed = %+v, %v", result, err)
	}
	if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
		t.Fatalf("finish zero-pending manifest: %v", err)
	}
	state, err := checkpoint.Inspect(testContext, provenance, identity)
	if err != nil || state.Status != RunCompleted || state.SeedManifest {
		t.Fatalf("zero-pending completed state = %+v, %v", state, err)
	}
}

func TestSeedManifestRejectsInvalidBatchesAndLegacyCollision(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	page := testPage("https://example.com/page", "example.com", "observation", 0)
	requireInvalidSeedManifestDefinitions(t, checkpoint, page)
	requireInvalidSeedManifestBatches(t, checkpoint, page)
}

func requireInvalidSeedManifestDefinitions(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	page Page,
) {
	t.Helper()
	if err := checkpoint.BeginSeedManifest(
		testContext,
		nil,
		[]byte("identity"),
		yagocrawlcontract.CrawlOrderPriorityNormal,
		[]Page{page},
	); !errors.Is(err, ErrInvalidProvenance) {
		t.Fatalf("empty manifest provenance error = %v", err)
	}
	if err := checkpoint.BeginSeedManifest(
		testContext,
		[]byte("identity"),
		nil,
		yagocrawlcontract.CrawlOrderPriorityNormal,
		[]Page{page},
	); !errors.Is(err, ErrInvalidIdentity) {
		t.Fatalf("empty manifest identity error = %v", err)
	}
	invalid := page
	invalid.ObservationID = ""
	if err := checkpoint.BeginSeedManifest(
		testContext,
		[]byte("invalid-page"),
		[]byte("identity"),
		yagocrawlcontract.CrawlOrderPriorityNormal,
		[]Page{invalid},
	); !errors.Is(err, ErrInvalidPage) {
		t.Fatalf("invalid manifest page error = %v", err)
	}
	invalidTime := page
	invalidTime.ObservedAt = time.Date(10_000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := checkpoint.BeginSeedManifest(
		testContext,
		[]byte("invalid-time"),
		[]byte("identity"),
		yagocrawlcontract.CrawlOrderPriorityNormal,
		[]Page{invalidTime},
	); err == nil {
		t.Fatal("unencodable manifest time succeeded")
	}
	legacy := []byte("legacy-manifest")
	beginTestRun(t, checkpoint, legacy, []byte("legacy-identity"))
	if err := checkpoint.BeginSeedManifest(
		testContext,
		legacy,
		[]byte("legacy-identity"),
		yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
		[]Page{page},
	); !errors.Is(err, ErrSeedManifestMissing) {
		t.Fatalf("legacy manifest collision error = %v", err)
	}
}

func requireInvalidSeedManifestBatches(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	page Page,
) {
	t.Helper()
	provenance := []byte("invalid-batches")
	identity := beginSeedManifest(t, checkpoint, provenance, []Page{page})
	if err := checkpoint.BeginSeedManifest(
		testContext,
		provenance,
		identity,
		yagocrawlcontract.CrawlOrderPriorityNormal,
		nil,
	); err != nil {
		t.Fatalf("reopen existing seed manifest: %v", err)
	}
	if err := checkpoint.BeginSeedManifest(
		testContext,
		provenance,
		[]byte("changed-identity"),
		yagocrawlcontract.CrawlOrderPriorityNormal,
		nil,
	); !errors.Is(err, ErrProvenanceCollision) {
		t.Fatalf("changed manifest identity error = %v", err)
	}
	tooLarge := make([]SeedDecision, SeedAdmissionBatchSize+1)
	for index := range tooLarge {
		tooLarge[index] = SeedDecision{Page: page}
	}
	cases := []SeedBatch{
		{},
		{Decisions: tooLarge},
		{Cursor: 1, Decisions: []SeedDecision{{Page: page}}},
		{
			Decisions: []SeedDecision{
				{Page: testPage("https://changed.example/", "changed.example", "changed", 0)},
			},
		},
	}
	for index, batch := range cases {
		if _, err := checkpoint.AdmitSeedBatch(testContext, provenance, batch); err == nil {
			t.Fatalf("invalid seed batch %d succeeded", index)
		}
	}
	if _, err := checkpoint.AdmitSeedBatch(testContext, nil, SeedBatch{
		Decisions: []SeedDecision{{Page: page}},
	}); !errors.Is(err, ErrInvalidProvenance) {
		t.Fatalf("empty seed batch provenance error = %v", err)
	}
	if _, err := checkpoint.AdmitSeedBatch(testContext, []byte("missing-run"), SeedBatch{
		Decisions: []SeedDecision{{Page: page}},
	}); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("missing seed batch run error = %v", err)
	}
	invalidDecision := page
	invalidDecision.Host = ""
	if _, err := checkpoint.AdmitSeedBatch(testContext, provenance, SeedBatch{
		Decisions: []SeedDecision{{Page: invalidDecision}},
	}); !errors.Is(err, ErrInvalidPage) {
		t.Fatalf("invalid seed decision error = %v", err)
	}
	if _, err := checkpoint.Admit(
		testContext,
		provenance,
		[]Page{page},
	); !errors.Is(err, ErrInvalidSeedBatch) {
		t.Fatalf("ordinary admission bypass error = %v", err)
	}
}

func TestSeedManifestOperationsRejectMissingBucketsAndRunStates(t *testing.T) {
	page := testPage("https://example.com/page", "example.com", "observation", 0)
	cases := []struct {
		name string
		run  func(*testing.T, Page)
	}{
		{name: "begin manifest bucket", run: rejectMissingManifestBucket},
		{name: "corrupt run", run: rejectCorruptManifestRun},
		{name: "admission manifest bucket", run: rejectMissingAdmissionManifestBucket},
		{name: "admission frontier bucket", run: rejectMissingAdmissionFrontierBucket},
		{name: "completed", run: rejectCompletedManifestAdmission},
		{name: "nonseeding", run: rejectNonseedingManifestAdmission},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) { testCase.run(t, page) })
	}
}

func rejectMissingManifestBucket(t *testing.T, page Page) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	deleteSchemaBucket(t, checkpoint, seedManifestBucket)
	if err := checkpoint.BeginSeedManifest(
		testContext,
		[]byte("missing-manifest-bucket"),
		[]byte("identity"),
		yagocrawlcontract.CrawlOrderPriorityNormal,
		[]Page{page},
	); !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("missing manifest bucket begin error = %v", err)
	}
}

func rejectCorruptManifestRun(t *testing.T, page Page) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
		return transaction.Bucket(runsBucket).Put([]byte("corrupt-run"), []byte("{"))
	})
	if err := checkpoint.BeginSeedManifest(
		testContext,
		[]byte("corrupt-run"),
		[]byte("identity"),
		yagocrawlcontract.CrawlOrderPriorityNormal,
		[]Page{page},
	); !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("corrupt manifest run error = %v", err)
	}
}

func rejectMissingAdmissionManifestBucket(t *testing.T, page Page) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("missing-admission-manifest")
	beginSeedManifest(t, checkpoint, provenance, []Page{page})
	deleteSchemaBucket(t, checkpoint, seedManifestBucket)
	if _, err := checkpoint.AdmitSeedBatch(testContext, provenance, SeedBatch{
		Decisions: []SeedDecision{{Page: page, Admit: true}},
	}); !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("missing admission manifest bucket error = %v", err)
	}
}

func rejectMissingAdmissionFrontierBucket(t *testing.T, page Page) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("missing-admission-frontier")
	beginSeedManifest(t, checkpoint, provenance, []Page{page})
	deleteSchemaBucket(t, checkpoint, visitedBucket)
	if _, err := checkpoint.AdmitSeedBatch(testContext, provenance, SeedBatch{
		Decisions: []SeedDecision{{Page: page, Admit: true}},
	}); !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("missing admission frontier bucket error = %v", err)
	}
}

func rejectCompletedManifestAdmission(t *testing.T, page Page) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("completed-manifest")
	beginSeedManifest(t, checkpoint, provenance, nil)
	if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
		t.Fatalf("finish empty manifest: %v", err)
	}
	if _, err := checkpoint.AdmitSeedBatch(testContext, provenance, SeedBatch{
		Decisions: []SeedDecision{{Page: page}},
	}); !errors.Is(err, ErrRunCompleted) {
		t.Fatalf("completed manifest admission error = %v", err)
	}
}

func rejectNonseedingManifestAdmission(t *testing.T, page Page) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("nonseeding-manifest")
	beginTestRun(t, checkpoint, provenance, []byte("identity"))
	if admitted, err := checkpoint.Admit(
		testContext,
		provenance,
		[]Page{page},
	); err != nil || admitted != 1 {
		t.Fatalf("admit nonseeding page = %d, %v", admitted, err)
	}
	if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
		t.Fatalf("finish legacy seeding: %v", err)
	}
	if _, err := checkpoint.AdmitSeedBatch(testContext, provenance, SeedBatch{
		Decisions: []SeedDecision{{Page: page}},
	}); !errors.Is(err, ErrSeedManifestMissing) {
		t.Fatalf("nonseeding manifest admission error = %v", err)
	}
}

func TestSeedManifestAdmissionRejectsCorruptRowsAndTallyOverflow(t *testing.T) {
	page := testPage("https://example.com/page", "example.com", "observation", 0)
	t.Run("missing row", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		provenance := []byte("missing-manifest-row")
		beginSeedManifest(t, checkpoint, provenance, []Page{page})
		prefix, _ := provenancePrefix(provenance)
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return transaction.Bucket(seedManifestBucket).Delete(sequenceRowKey(prefix, 1))
		})
		if _, err := checkpoint.AdmitSeedBatch(testContext, provenance, SeedBatch{
			Decisions: []SeedDecision{{Page: page, Admit: true}},
		}); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("missing manifest row admission error = %v", err)
		}
	})
	t.Run("invalid row", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		provenance := []byte("invalid-manifest-row")
		beginSeedManifest(t, checkpoint, provenance, []Page{page})
		prefix, _ := provenancePrefix(provenance)
		invalid := page
		invalid.ObservationID = ""
		encoded, err := encodeRow("seed manifest page", invalid)
		if err != nil {
			t.Fatalf("encode invalid manifest row: %v", err)
		}
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return transaction.Bucket(seedManifestBucket).Put(sequenceRowKey(prefix, 1), encoded)
		})
		if _, err := checkpoint.AdmitSeedBatch(testContext, provenance, SeedBatch{
			Decisions: []SeedDecision{{Page: page, Admit: true}},
		}); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("invalid manifest row admission error = %v", err)
		}
	})
	t.Run("visited admission", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		provenance := []byte("visited-manifest-admission")
		duplicate := page
		duplicate.ObservationID = "duplicate"
		beginSeedManifest(t, checkpoint, provenance, []Page{page, duplicate})
		if _, err := checkpoint.AdmitSeedBatch(testContext, provenance, SeedBatch{
			Decisions: []SeedDecision{{Page: page, Admit: true}},
		}); err != nil {
			t.Fatalf("admit first manifest page: %v", err)
		}
		if _, err := checkpoint.AdmitSeedBatch(testContext, provenance, SeedBatch{
			Cursor:    1,
			Decisions: []SeedDecision{{Page: duplicate, Admit: true}},
		}); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("visited manifest admission error = %v", err)
		}
	})
	t.Run("tally overflow", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		provenance := []byte("manifest-tally-overflow")
		duplicate := page
		duplicate.ObservationID = "duplicate"
		beginSeedManifest(t, checkpoint, provenance, []Page{page, duplicate})
		if _, err := checkpoint.AdmitSeedBatch(testContext, provenance, SeedBatch{
			Decisions: []SeedDecision{{Page: page, Admit: true}},
		}); err != nil {
			t.Fatalf("admit tally source page: %v", err)
		}
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			record, _, err := readRunRecord(transaction, provenance)
			if err != nil {
				return err
			}
			record.Tally.Duplicates = ^uint64(0)
			return writeRunRecord(transaction, provenance, record)
		})
		if _, err := checkpoint.AdmitSeedBatch(testContext, provenance, SeedBatch{
			Cursor:    1,
			Decisions: []SeedDecision{{Page: duplicate}},
		}); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("manifest tally overflow error = %v", err)
		}
	})
}

func TestSeedManifestAdmissionRollsBackCursorAndTally(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("manifest-rollback")
	page := testPage("https://broken.example/page", "broken.example", "broken", 0)
	beginSeedManifest(t, checkpoint, provenance, []Page{page})
	prefix, _ := provenancePrefix(provenance)
	mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
		return transaction.Bucket(hostsBucket).Put(
			childRowKey(prefix, page.Host),
			[]byte("{"),
		)
	})
	if _, err := checkpoint.AdmitSeedBatch(testContext, provenance, SeedBatch{
		Decisions: []SeedDecision{{Page: page, Admit: true}},
	}); !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("corrupt host seed admission error = %v", err)
	}
	snapshot, err := checkpoint.Load(testContext, provenance)
	if err == nil || snapshot.SeedCursor != 0 || snapshot.Tally.Duplicates != 0 {
		t.Fatalf("rolled back manifest snapshot = %+v, %v", snapshot, err)
	}
}

func TestSeedManifestLoadRejectsCorruptRowsAndMetadata(t *testing.T) {
	page := testPage("https://example.com/page", "example.com", "observation", 0)
	cases := []struct {
		name   string
		mutate func(*bolt.Tx, []byte, []byte) error
	}{
		{name: "missing row", mutate: func(transaction *bolt.Tx, prefix, _ []byte) error {
			return transaction.Bucket(seedManifestBucket).Delete(sequenceRowKey(prefix, 1))
		}},
		{name: "malformed row", mutate: func(transaction *bolt.Tx, prefix, _ []byte) error {
			return transaction.Bucket(seedManifestBucket).
				Put(sequenceRowKey(prefix, 1), []byte("{"))
		}},
		{name: "excess row", mutate: func(transaction *bolt.Tx, prefix, _ []byte) error {
			encoded, err := encodeRow("seed manifest page", page)
			if err != nil {
				return err
			}
			return transaction.Bucket(seedManifestBucket).Put(sequenceRowKey(prefix, 2), encoded)
		}},
		{name: "cursor", mutate: func(transaction *bolt.Tx, _, provenance []byte) error {
			record, _, err := readRunRecord(transaction, provenance)
			if err != nil {
				return err
			}
			record.SeedCursor = 2
			return writeRunRecord(transaction, provenance, record)
		}},
		{name: "marker", mutate: func(transaction *bolt.Tx, _, provenance []byte) error {
			record, _, err := readRunRecord(transaction, provenance)
			if err != nil {
				return err
			}
			record.SeedManifest = false
			return writeRunRecord(transaction, provenance, record)
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
			provenance := []byte("corrupt-manifest")
			beginSeedManifest(t, checkpoint, provenance, []Page{page})
			prefix, _ := provenancePrefix(provenance)
			mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
				return testCase.mutate(transaction, prefix, provenance)
			})
			if _, err := checkpoint.Load(testContext, provenance); !errors.Is(
				err,
				ErrCorruptCheckpoint,
			) {
				t.Fatalf("corrupt manifest load error = %v", err)
			}
		})
	}
}

func TestOpenMigratesHostPaceSchemaToSeedManifest(t *testing.T) {
	path := testCheckpointPath(t)
	writeRawCheckpoint(t, path, func(transaction *bolt.Tx) error {
		version := make([]byte, 4)
		binary.BigEndian.PutUint32(version, hostPaceSchemaVersion)
		putSchemaVersion(t, transaction, version)
		for _, name := range append(
			append([][]byte(nil), initialSchemaBuckets[1:]...),
			hostPacesBucket,
			hostPaceOrderBucket,
		) {
			if _, err := transaction.CreateBucket(name); err != nil {
				return fmt.Errorf("create host pace schema bucket: %w", err)
			}
		}
		return nil
	})
	checkpoint := openTestCheckpoint(t, path)
	if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
		if transaction.Bucket(seedManifestBucket) == nil {
			t.Fatal("migrated seed manifest bucket is missing")
		}
		version := transaction.Bucket(metadataBucket).Get(schemaVersionKey)
		if len(version) != 4 || binary.BigEndian.Uint32(version) != currentSchemaVersion {
			t.Fatalf("migrated schema version = %v", version)
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect migrated seed manifest schema: %v", err)
	}
}
