package frontiercheckpoint

import (
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestCancelQueuedPagesCompletesQueuedRun(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("cancel-queued")
	identity := []byte("cancel-queued-order")
	pages := checkpointTransitionTestPages("queued.example", 3)
	beginTestRun(t, checkpoint, provenance, identity)
	if admitted, err := checkpoint.Admit(
		testContext,
		provenance,
		pages,
	); err != nil ||
		admitted != len(pages) {
		t.Fatalf("admit cancellation pages = %d, %v", admitted, err)
	}
	if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
		t.Fatalf("finish cancellation seeding: %v", err)
	}
	if err := checkpoint.UpdateControl(
		testContext,
		provenance,
		ControlUpdate{Cancelled: true},
	); err != nil {
		t.Fatalf("mark cancellation: %v", err)
	}
	if err := checkpoint.CancelQueuedPages(
		testContext,
		provenance,
		[]string{pages[0].URL, pages[1].URL, pages[2].URL, pages[0].URL},
	); err != nil {
		t.Fatalf("cancel queued pages: %v", err)
	}
	state, err := checkpoint.Inspect(testContext, provenance, identity)
	if err != nil {
		t.Fatalf("inspect cancelled run: %v", err)
	}
	if state.Status != RunCompleted || state.Pending != 0 || !state.Control.Cancelled ||
		state.Tally != (yagocrawlcontract.CrawlRunTally{}) {
		t.Fatalf("cancelled run state = %+v", state)
	}
}

func TestCancelQueuedPagesRetainsInflightPage(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("cancel-inflight")
	identity := []byte("cancel-inflight-order")
	pages := checkpointTransitionTestPages("inflight.example", 3)
	beginTestRun(t, checkpoint, provenance, identity)
	if admitted, err := checkpoint.Admit(
		testContext,
		provenance,
		pages,
	); err != nil ||
		admitted != len(pages) {
		t.Fatalf("admit inflight pages = %d, %v", admitted, err)
	}
	if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
		t.Fatalf("finish inflight seeding: %v", err)
	}
	if err := checkpoint.UpdateControl(
		testContext,
		provenance,
		ControlUpdate{Cancelled: true},
	); err != nil {
		t.Fatalf("mark inflight cancellation: %v", err)
	}
	if err := checkpoint.CancelQueuedPages(
		testContext,
		provenance,
		[]string{pages[1].URL, pages[2].URL},
	); err != nil {
		t.Fatalf("cancel queued around inflight page: %v", err)
	}
	snapshot, err := checkpoint.Load(testContext, provenance)
	if err != nil {
		t.Fatalf("load draining cancellation: %v", err)
	}
	if snapshot.Completed || snapshot.Counters.Pending != 1 || len(snapshot.Outstanding) != 1 ||
		snapshot.Outstanding[0].URL != pages[0].URL {
		t.Fatalf("draining cancellation snapshot = %+v", snapshot)
	}
	tally := yagocrawlcontract.CrawlRunTally{Fetched: 1, Indexed: 1}
	if err := checkpoint.CompletePage(
		testContext,
		provenance,
		pages[0].URL,
		PageCompletion{Tally: tally},
	); err != nil {
		t.Fatalf("complete inflight cancellation page: %v", err)
	}
	state, err := checkpoint.Inspect(testContext, provenance, identity)
	if err != nil || state.Status != RunCompleted || state.Tally != tally {
		t.Fatalf("completed inflight cancellation = %+v, %v", state, err)
	}
}

func TestCancelledRunResumesMidTransition(t *testing.T) {
	path := testCheckpointPath(t)
	checkpoint, err := Open(path)
	if err != nil {
		t.Fatalf("open cancellation transition: %v", err)
	}
	provenance := []byte("cancel-mid-transition")
	identity := []byte("cancel-mid-transition-order")
	pages := checkpointTransitionTestPages("cancel-mid.example", cancellationPagesPerTransaction+17)
	beginTestRun(t, checkpoint, provenance, identity)
	if admitted, err := checkpoint.Admit(
		testContext,
		provenance,
		pages,
	); err != nil ||
		admitted != len(pages) {
		t.Fatalf("admit transition pages = %d, %v", admitted, err)
	}
	if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
		t.Fatalf("finish transition seeding: %v", err)
	}
	if err := checkpoint.UpdateControl(
		testContext,
		provenance,
		ControlUpdate{Cancelled: true},
	); err != nil {
		t.Fatalf("mark transition cancellation: %v", err)
	}
	prefix, _ := provenancePrefix(provenance)
	pageURLs := checkpointPageURLs(pages)
	if err := checkpoint.cancelQueuedPageChunk(
		testContext,
		provenance,
		prefix,
		pageURLs[:cancellationPagesPerTransaction],
	); err != nil {
		t.Fatalf("cancel first transition chunk: %v", err)
	}
	state, err := checkpoint.Inspect(testContext, provenance, identity)
	if err != nil || state.Pending != 17 || state.Status != RunActive {
		t.Fatalf("partial cancellation state = %+v, %v", state, err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close partial cancellation: %v", err)
	}
	checkpoint, err = Open(path)
	if err != nil {
		t.Fatalf("reopen partial cancellation: %v", err)
	}
	t.Cleanup(func() { _ = checkpoint.Close() })
	state, err = checkpoint.Inspect(testContext, provenance, identity)
	if err != nil || state.Status != RunCompleted || state.Pending != 0 ||
		!state.Control.Cancelled {
		t.Fatalf("recovered cancellation state = %+v, %v", state, err)
	}
}

func TestCancelledSeedingManifestResumesAfterReopen(t *testing.T) {
	path := testCheckpointPath(t)
	checkpoint, err := Open(path)
	if err != nil {
		t.Fatalf("open cancelled seeding transition: %v", err)
	}
	provenance := []byte("cancel-seeding-transition")
	identity := []byte("cancel-seeding-transition-order")
	pages := checkpointTransitionTestPages("cancel-seeding.example", SeedAdmissionBatchSize+9)
	if err := checkpoint.BeginSeedManifest(
		testContext,
		provenance,
		identity,
		yagocrawlcontract.CrawlOrderPriorityNormal,
		pages,
	); err != nil {
		t.Fatalf("begin cancelled seed manifest: %v", err)
	}
	decisions := make([]SeedDecision, 0, 11)
	for _, page := range pages[:11] {
		decisions = append(decisions, SeedDecision{Page: page, Admit: true})
	}
	if result, err := checkpoint.AdmitSeedBatch(
		testContext,
		provenance,
		SeedBatch{Decisions: decisions},
	); err != nil || result.Admitted != len(decisions) {
		t.Fatalf("admit cancelled seed batch = %+v, %v", result, err)
	}
	if err := checkpoint.UpdateControl(
		testContext,
		provenance,
		ControlUpdate{Cancelled: true},
	); err != nil {
		t.Fatalf("mark seeding cancellation: %v", err)
	}
	if err := checkpoint.CancelQueuedPages(
		testContext,
		provenance,
		checkpointPageURLs(pages[:5]),
	); err != nil {
		t.Fatalf("cancel first seeded pages: %v", err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close cancelled seeding transition: %v", err)
	}
	checkpoint, err = Open(path)
	if err != nil {
		t.Fatalf("reopen cancelled seeding transition: %v", err)
	}
	t.Cleanup(func() { _ = checkpoint.Close() })
	snapshot, err := checkpoint.Load(testContext, provenance)
	if err != nil {
		t.Fatalf("load recovered cancelled seeding: %v", err)
	}
	if !snapshot.Completed || snapshot.Seeding || snapshot.SeedManifest ||
		snapshot.Counters.Pending != 0 || len(snapshot.Outstanding) != 0 ||
		!snapshot.Control.Cancelled {
		t.Fatalf("recovered cancelled seeding snapshot = %+v", snapshot)
	}
}

func TestCancelledRunDoesNotMutateInterleavedRun(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	cancelledProvenance := []byte("cancel-interleaved")
	keptProvenance := []byte("keep-interleaved")
	cancelledPages := checkpointTransitionTestPages("shared.example", 3)
	keptPages := checkpointTransitionTestPages("shared.example", 2)
	beginTestRun(t, checkpoint, cancelledProvenance, []byte("cancel-interleaved-order"))
	beginTestRun(t, checkpoint, keptProvenance, []byte("keep-interleaved-order"))
	if admitted, err := checkpoint.Admit(
		testContext,
		cancelledProvenance,
		cancelledPages,
	); err != nil ||
		admitted != 3 {
		t.Fatalf("admit cancelled interleaved pages = %d, %v", admitted, err)
	}
	if admitted, err := checkpoint.Admit(
		testContext,
		keptProvenance,
		keptPages,
	); err != nil ||
		admitted != 2 {
		t.Fatalf("admit kept interleaved pages = %d, %v", admitted, err)
	}
	if err := checkpoint.FinishSeeding(
		testContext,
		cancelledProvenance,
		testRunTally(),
	); err != nil {
		t.Fatalf("finish cancelled interleaved seeding: %v", err)
	}
	if err := checkpoint.FinishSeeding(testContext, keptProvenance, testRunTally()); err != nil {
		t.Fatalf("finish kept interleaved seeding: %v", err)
	}
	if err := checkpoint.UpdateControl(
		testContext,
		cancelledProvenance,
		ControlUpdate{Cancelled: true},
	); err != nil {
		t.Fatalf("mark interleaved cancellation: %v", err)
	}
	if err := checkpoint.CancelQueuedPages(
		testContext,
		cancelledProvenance,
		checkpointPageURLs(cancelledPages),
	); err != nil {
		t.Fatalf("cancel interleaved pages: %v", err)
	}
	kept, err := checkpoint.Load(testContext, keptProvenance)
	if err != nil {
		t.Fatalf("load kept interleaved run: %v", err)
	}
	if kept.Completed || kept.Counters.Pending != 2 || len(kept.Outstanding) != 2 ||
		kept.Control.Cancelled {
		t.Fatalf("kept interleaved run = %+v", kept)
	}
}

func checkpointTransitionTestPages(host string, total int) []Page {
	pages := make([]Page, 0, total)
	for index := range total {
		pages = append(pages, testPage(
			fmt.Sprintf("https://%s/page/%04d", host, index),
			host,
			fmt.Sprintf("observation-%04d", index),
			index,
		))
	}

	return pages
}

func checkpointPageURLs(pages []Page) []string {
	pageURLs := make([]string, 0, len(pages))
	for _, page := range pages {
		pageURLs = append(pageURLs, page.URL)
	}

	return pageURLs
}
