package frontiercheckpoint

import (
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestBoundedRecoveryPagesStayWithinRequestedWindow(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("bounded-large-run")
	beginTestRun(t, checkpoint, provenance, []byte("bounded-large-order"))
	pages := boundedRecoveryTestPages(1_025)
	admitCheckpointTestPages(t, checkpoint, provenance, pages)
	if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
		t.Fatalf("finish bounded recovery seeding: %v", err)
	}

	snapshot, err := checkpoint.LoadBounded(testContext, provenance, 64)
	if err != nil {
		t.Fatalf("load bounded recovery: %v", err)
	}
	if !snapshot.RecoveryBounded || snapshot.RecoveryComplete ||
		len(snapshot.Outstanding) != 64 || len(snapshot.HostStates) != 64 ||
		len(snapshot.Visited) != 0 || snapshot.Counters.Pending != uint64(len(pages)) {
		t.Fatalf("initial bounded recovery = %+v", snapshot)
	}
	if snapshot.RecoveryCursor == 0 || snapshot.RecoveryCursor >= snapshot.RecoveryUpper {
		t.Fatalf("initial recovery cursor = %d/%d", snapshot.RecoveryCursor, snapshot.RecoveryUpper)
	}

	admittedLater := boundedRecoveryTestPages(1)[0]
	admittedLater.URL = "https://later.example/page"
	admittedLater.Host = "later.example"
	admittedLater.ObservationID = "later-observation"
	if admitted, err := checkpoint.Admit(
		testContext,
		provenance,
		[]Page{admittedLater},
	); err != nil ||
		admitted != 1 {
		t.Fatalf("admit page above recovery boundary = %d, %v", admitted, err)
	}

	total := len(snapshot.Outstanding)
	cursor := snapshot.RecoveryCursor
	for cursor < snapshot.RecoveryUpper {
		batch, err := checkpoint.LoadRecoveryPageBatch(
			testContext,
			provenance,
			cursor,
			snapshot.RecoveryUpper,
			64,
		)
		if err != nil {
			t.Fatalf("load recovery batch after %d: %v", cursor, err)
		}
		if len(batch.Pages) > 64 || batch.Cursor <= cursor ||
			batch.Cursor > snapshot.RecoveryUpper {
			t.Fatalf("recovery batch after %d = %+v", cursor, batch)
		}
		for _, page := range batch.Pages {
			if page.URL == admittedLater.URL {
				t.Fatal("post-restart admission crossed the fixed recovery boundary")
			}
		}
		total += len(batch.Pages)
		cursor = batch.Cursor
	}
	if total != len(pages) {
		t.Fatalf("recovered pages = %d, want %d", total, len(pages))
	}
}

func TestBoundedRecoveryDropsInitiallyRetiredPagesBeforePendingIsTracked(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("bounded-retired-run")
	host := "retired.example"
	beginTestRun(t, checkpoint, provenance, []byte("bounded-retired-order"))
	pages := checkpointTransitionTestPages(host, RecoveryPageBatchSize+37)
	admitCheckpointTestPages(t, checkpoint, provenance, pages)
	if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
		t.Fatalf("finish retired recovery seeding: %v", err)
	}
	if err := checkpoint.RecordHostState(
		testContext,
		provenance,
		host,
		HostProgress{Generation: 1, Retired: true},
		nil,
	); err != nil {
		t.Fatalf("mark recovery host retired: %v", err)
	}

	snapshot, err := checkpoint.LoadBounded(testContext, provenance, RecoveryPageBatchSize)
	if err != nil {
		t.Fatalf("load retired bounded recovery: %v", err)
	}
	var expectedPending uint64
	for range pages[RecoveryPageBatchSize:] {
		expectedPending++
	}
	if len(snapshot.Outstanding) != 0 ||
		snapshot.Counters.Pending != expectedPending ||
		!snapshot.HostStates[host].Retired {
		t.Fatalf("retired initial recovery = %+v", snapshot)
	}
	batch, err := checkpoint.LoadRecoveryPageBatch(
		testContext,
		provenance,
		snapshot.RecoveryCursor,
		snapshot.RecoveryUpper,
		RecoveryPageBatchSize,
	)
	if err != nil {
		t.Fatalf("drop remaining retired recovery pages: %v", err)
	}
	if len(batch.Pages) != 0 || batch.RetiredPages != 37 || !batch.Complete {
		t.Fatalf("retired recovery batch = %+v", batch)
	}
	state, err := checkpoint.Inspect(testContext, provenance, []byte("bounded-retired-order"))
	if err != nil || state.Status != RunCompleted || state.Pending != 0 {
		t.Fatalf("retired recovered state = %+v, %v", state, err)
	}
}

func TestBoundedSeedManifestReadsOnlyOneAdmissionWindow(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("bounded-seed-run")
	pages := boundedRecoveryTestPages(RecoveryPageBatchSize*2 + 9)
	if err := checkpoint.BeginSeedManifest(
		testContext,
		provenance,
		[]byte("bounded-seed-order"),
		yagocrawlcontract.CrawlOrderPriorityNormal,
		pages,
	); err != nil {
		t.Fatalf("begin bounded seed manifest: %v", err)
	}

	cursor := uint64(0)
	total := 0
	for {
		loaded, next, complete, err := checkpoint.LoadSeedPageBatch(
			testContext,
			provenance,
			cursor,
			RecoveryPageBatchSize,
		)
		if err != nil {
			t.Fatalf("load seed batch after %d: %v", cursor, err)
		}
		if len(loaded) > RecoveryPageBatchSize || next <= cursor {
			t.Fatalf("seed batch after %d = %d pages, cursor %d", cursor, len(loaded), next)
		}
		total += len(loaded)
		cursor = next
		decisions := make([]SeedDecision, 0, len(loaded))
		for _, page := range loaded {
			decisions = append(decisions, SeedDecision{Page: page, Admit: true})
		}
		if _, err := checkpoint.AdmitSeedBatch(
			testContext,
			provenance,
			SeedBatch{Cursor: cursor - uint64(len(loaded)), Decisions: decisions},
		); err != nil {
			t.Fatalf("admit seed batch ending at %d: %v", cursor, err)
		}
		if complete {
			break
		}
	}
	if total != len(pages) {
		t.Fatalf("seed pages = %d, want %d", total, len(pages))
	}
}

func boundedRecoveryTestPages(total int) []Page {
	pages := make([]Page, 0, total)
	for index := range total {
		host := fmt.Sprintf("host-%05d.example", index)
		pages = append(pages, testPage(
			fmt.Sprintf("https://%s/page", host),
			host,
			fmt.Sprintf("bounded-observation-%05d", index),
			index,
		))
	}

	return pages
}
