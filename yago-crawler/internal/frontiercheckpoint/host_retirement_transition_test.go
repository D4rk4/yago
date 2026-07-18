package frontiercheckpoint

import (
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestHostRetirementUsesBoundedChunks(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("bounded-host-retirement")
	identity := []byte("bounded-host-retirement-order")
	retiredPages := checkpointTransitionTestPages(
		"retired.example",
		retirementPagesPerTransaction*2+1,
	)
	kept := testPage("https://kept.example/page", "kept.example", "kept", 0)
	pages := append(append([]Page(nil), retiredPages...), kept)
	beginTestRun(t, checkpoint, provenance, identity)
	admitCheckpointTestPages(t, checkpoint, provenance, pages)
	if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
		t.Fatalf("finish bounded retirement seeding: %v", err)
	}
	if err := checkpoint.RecordHostState(
		testContext,
		provenance,
		"retired.example",
		HostProgress{Generation: 7, Failures: 5, Retired: true},
		checkpointPageURLs(retiredPages),
	); err != nil {
		t.Fatalf("retire bounded host pages: %v", err)
	}
	snapshot, err := checkpoint.Load(testContext, provenance)
	if err != nil {
		t.Fatalf("load bounded retirement: %v", err)
	}
	if snapshot.Completed || snapshot.Counters.Pending != 1 || len(snapshot.Outstanding) != 1 ||
		snapshot.Outstanding[0].URL != kept.URL {
		t.Fatalf("bounded retirement snapshot = %+v", snapshot)
	}
}

func TestHostRetirementResumesAfterReopen(t *testing.T) {
	path := testCheckpointPath(t)
	checkpoint, err := Open(path)
	if err != nil {
		t.Fatalf("open retirement transition: %v", err)
	}
	provenance := []byte("resume-host-retirement")
	identity := []byte("resume-host-retirement-order")
	retiredPages := checkpointTransitionTestPages(
		"resume-retired.example",
		retirementPagesPerTransaction+44,
	)
	keptPages := checkpointTransitionTestPages("resume-kept.example", 20)
	pages := append(append([]Page(nil), retiredPages...), keptPages...)
	beginTestRun(t, checkpoint, provenance, identity)
	admitCheckpointTestPages(t, checkpoint, provenance, pages)
	if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
		t.Fatalf("finish resumable retirement seeding: %v", err)
	}
	if err := checkpoint.RecordHostState(
		testContext,
		provenance,
		"resume-retired.example",
		HostProgress{Generation: 3, Failures: 5, Retired: true},
		nil,
	); err != nil {
		t.Fatalf("mark resumable host retirement: %v", err)
	}
	prefix, _ := provenancePrefix(provenance)
	done, err := checkpoint.resumeRetiredHostTransitionChunk(
		testContext,
		provenance,
		prefix,
		"resume-retired.example",
	)
	if err != nil || done {
		t.Fatalf("first retirement recovery chunk = done %t, %v", done, err)
	}
	record := loadTestHostRecord(t, checkpoint, prefix, "resume-retired.example")
	if record.RetirementCursor != retirementPagesPerTransaction || record.RetirementScanned {
		t.Fatalf("bounded retirement cursor = %+v", record)
	}
	partial, err := checkpoint.Load(testContext, provenance)
	if err != nil || partial.Counters.Pending != uint64(44+len(keptPages)) {
		t.Fatalf("partial retirement snapshot = %+v, %v", partial, err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close partial retirement: %v", err)
	}
	checkpoint, err = Open(path)
	if err != nil {
		t.Fatalf("reopen partial retirement: %v", err)
	}
	t.Cleanup(func() { _ = checkpoint.Close() })
	snapshot, err := checkpoint.Load(testContext, provenance)
	if err != nil {
		t.Fatalf("load recovered retirement: %v", err)
	}
	if snapshot.Counters.Pending != uint64(len(keptPages)) ||
		len(snapshot.Outstanding) != len(keptPages) {
		t.Fatalf("recovered retirement snapshot = %+v", snapshot)
	}
	for _, page := range snapshot.Outstanding {
		if page.Host != "resume-kept.example" {
			t.Fatalf("retired host page survived recovery: %+v", page)
		}
	}
}

func TestHostRetirementDoesNotMutateInterleavedHostsOrRuns(t *testing.T) {
	path := testCheckpointPath(t)
	checkpoint, err := Open(path)
	if err != nil {
		t.Fatalf("open interleaved retirement: %v", err)
	}
	retiredProvenance := []byte("retired-interleaved-run")
	keptProvenance := []byte("kept-interleaved-run")
	retiredHostPages := checkpointTransitionTestPages("interleaved.example", 3)
	otherHostPage := testPage("https://other.example/page", "other.example", "other-host", 0)
	keptRunPages := checkpointTransitionTestPages("interleaved.example", 2)
	beginTestRun(t, checkpoint, retiredProvenance, []byte("retired-interleaved-order"))
	beginTestRun(t, checkpoint, keptProvenance, []byte("kept-interleaved-order"))
	admitCheckpointTestPages(
		t,
		checkpoint,
		retiredProvenance,
		append(append([]Page(nil), retiredHostPages...), otherHostPage),
	)
	admitCheckpointTestPages(t, checkpoint, keptProvenance, keptRunPages)
	if err := checkpoint.FinishSeeding(testContext, retiredProvenance, testRunTally()); err != nil {
		t.Fatalf("finish retired interleaved seeding: %v", err)
	}
	if err := checkpoint.FinishSeeding(testContext, keptProvenance, testRunTally()); err != nil {
		t.Fatalf("finish kept interleaved seeding: %v", err)
	}
	if err := checkpoint.RecordHostState(
		testContext,
		retiredProvenance,
		"interleaved.example",
		HostProgress{Generation: 4, Failures: 5, Retired: true},
		nil,
	); err != nil {
		t.Fatalf("mark interleaved retirement: %v", err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close interleaved retirement: %v", err)
	}
	checkpoint, err = Open(path)
	if err != nil {
		t.Fatalf("reopen interleaved retirement: %v", err)
	}
	t.Cleanup(func() { _ = checkpoint.Close() })
	retiredRun, err := checkpoint.Load(testContext, retiredProvenance)
	if err != nil || retiredRun.Counters.Pending != 1 || len(retiredRun.Outstanding) != 1 ||
		retiredRun.Outstanding[0].URL != otherHostPage.URL {
		t.Fatalf("retired interleaved run = %+v, %v", retiredRun, err)
	}
	keptRun, err := checkpoint.Load(testContext, keptProvenance)
	if err != nil || keptRun.Counters.Pending != 2 || len(keptRun.Outstanding) != 2 {
		t.Fatalf("kept interleaved run = %+v, %v", keptRun, err)
	}
}

func TestHostRetirementReverseCompletionKeepsRecoveryCursor(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("reverse-retirement")
	identity := []byte("reverse-retirement-order")
	pages := checkpointTransitionTestPages(
		"reverse-retired.example",
		retirementPagesPerTransaction+1,
	)
	beginTestRun(t, checkpoint, provenance, identity)
	admitCheckpointTestPages(t, checkpoint, provenance, pages)
	if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
		t.Fatalf("finish reverse retirement seeding: %v", err)
	}
	if err := checkpoint.RecordHostState(
		testContext,
		provenance,
		"reverse-retired.example",
		HostProgress{Generation: 9, Failures: 5, Retired: true},
		nil,
	); err != nil {
		t.Fatalf("mark reverse retirement: %v", err)
	}
	prefix, _ := provenancePrefix(provenance)
	if done, err := checkpoint.resumeRetiredHostTransitionChunk(
		testContext,
		provenance,
		prefix,
		"reverse-retired.example",
	); err != nil || done {
		t.Fatalf("advance reverse retirement = done %t, %v", done, err)
	}
	if err := checkpoint.RecordHostState(
		testContext,
		provenance,
		"reverse-retired.example",
		HostProgress{Generation: 8, Failures: 1},
		nil,
	); err != nil {
		t.Fatalf("record reverse host completion: %v", err)
	}
	record := loadTestHostRecord(t, checkpoint, prefix, "reverse-retired.example")
	if record.Generation != 9 || !record.Retired || record.Failures != 5 ||
		record.RetirementCursor != retirementPagesPerTransaction || record.RetirementScanned {
		t.Fatalf("reverse retirement record = %+v", record)
	}
}

func TestCompletePageReplayDoesNotApplyHostProgress(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("replayed-page-host-progress")
	current := testPage("https://replay.example/current", "replay.example", "current", 0)
	kept := testPage("https://replay.example/kept", "replay.example", "kept", 0)
	beginTestRun(t, checkpoint, provenance, []byte("replayed-page-host-progress-order"))
	admitCheckpointTestPages(t, checkpoint, provenance, []Page{current, kept})
	if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
		t.Fatalf("finish replay seeding: %v", err)
	}
	firstTally := yagocrawlcontract.CrawlRunTally{Fetched: 1}
	if err := checkpoint.CompletePage(
		testContext,
		provenance,
		current.URL,
		PageCompletion{Tally: firstTally},
	); err != nil {
		t.Fatalf("complete replay target: %v", err)
	}
	if err := checkpoint.CompletePage(
		testContext,
		provenance,
		current.URL,
		PageCompletion{
			Tally: yagocrawlcontract.CrawlRunTally{Failed: 1},
			HostProgress: &PageHostProgress{
				Host:        current.Host,
				Progress:    HostProgress{Generation: 3, Failures: 5, Retired: true},
				DroppedURLs: []string{kept.URL},
			},
		},
	); err != nil {
		t.Fatalf("replay completed page: %v", err)
	}
	snapshot, err := checkpoint.Load(testContext, provenance)
	if err != nil {
		t.Fatalf("load page replay: %v", err)
	}
	if snapshot.Completed || snapshot.Counters.Pending != 1 || len(snapshot.Outstanding) != 1 ||
		snapshot.Outstanding[0].URL != kept.URL || snapshot.Tally != firstTally ||
		snapshot.HostStates[current.Host].Retired {
		t.Fatalf("page replay snapshot = %+v", snapshot)
	}
}

func admitCheckpointTestPages(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	provenance []byte,
	pages []Page,
) {
	t.Helper()
	for start := 0; start < len(pages); start += SeedAdmissionBatchSize {
		end := min(start+SeedAdmissionBatchSize, len(pages))
		admitted, err := checkpoint.Admit(testContext, provenance, pages[start:end])
		if err != nil || admitted != end-start {
			t.Fatalf("admit checkpoint test pages %d:%d = %d, %v", start, end, admitted, err)
		}
	}
}

func loadTestHostRecord(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	prefix []byte,
	host string,
) hostRecord {
	t.Helper()
	var record hostRecord
	if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
		var err error
		record, err = readHostRecord(transaction.Bucket(hostsBucket), prefix, host)

		return err
	}); err != nil {
		t.Fatalf("load host record: %v", err)
	}

	return record
}
