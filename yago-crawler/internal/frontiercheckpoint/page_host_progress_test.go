package frontiercheckpoint

import (
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlpace"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type hostGenerationFixture struct {
	checkpoint *FrontierCheckpoint
	provenance []byte
	older      Page
	newer      Page
	dropped    Page
}

func TestPageCompletionAtomicallyCommitsHostProgressAndDroppedQueue(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("atomic-host-completion")
	beginTestRun(t, checkpoint, provenance, []byte("atomic-host-completion-order"))
	current := testPage("https://busy.example/current", "busy.example", "current", 0)
	dropped := testPage("https://busy.example/dropped", "busy.example", "dropped", 0)
	if admitted, err := checkpoint.Admit(
		testContext,
		provenance,
		[]Page{current, dropped},
	); err != nil ||
		admitted != 2 {
		t.Fatalf("admit atomic host pages = %d, %v", admitted, err)
	}
	if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
		t.Fatalf("finish atomic host seeding: %v", err)
	}
	now := time.Date(2026, 7, 16, 18, 0, 0, 0, time.UTC)
	pace := crawlpace.HostState{
		NextDueAt:       now.Add(time.Second),
		BackoffUntil:    now.Add(15 * time.Minute),
		BackoffPenalty:  15 * time.Minute,
		BackoffFailures: 5,
		Generation:      9,
	}
	tally := yagocrawlcontract.CrawlRunTally{Fetched: 1, Failed: 1}
	if err := checkpoint.CompletePage(testContext, provenance, current.URL, PageCompletion{
		Tally: tally,
		HostProgress: &PageHostProgress{
			Host:        current.Host,
			Progress:    HostProgress{Retired: true, Pace: pace, PaceCapacity: 8},
			DroppedURLs: []string{dropped.URL},
		},
	}); err != nil {
		t.Fatalf("complete atomic host page: %v", err)
	}
	snapshot, err := checkpoint.Load(testContext, provenance)
	if err != nil {
		t.Fatalf("load atomic host completion: %v", err)
	}
	host := snapshot.HostStates[current.Host]
	if !snapshot.Completed || snapshot.Counters.Pending != 0 || len(snapshot.Outstanding) != 0 ||
		!host.Retired || host.Failures != 0 || snapshot.Tally != tally {
		t.Fatalf("atomic host completion snapshot = %+v", snapshot)
	}
	paces, err := checkpoint.HostPaces(testContext, 8)
	if err != nil || paces[current.Host] != pace {
		t.Fatalf("atomic host pace = %+v, %v", paces, err)
	}
}

func TestPageCompletionRollsBackHostProgressWithPageFailure(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("rollback-host-completion")
	beginTestRun(t, checkpoint, provenance, []byte("rollback-host-completion-order"))
	current := testPage("https://busy.example/current", "busy.example", "current", 0)
	other := testPage("https://other.example/page", "other.example", "other", 0)
	if admitted, err := checkpoint.Admit(
		testContext,
		provenance,
		[]Page{current, other},
	); err != nil ||
		admitted != 2 {
		t.Fatalf("admit rollback host pages = %d, %v", admitted, err)
	}
	if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
		t.Fatalf("finish rollback host seeding: %v", err)
	}
	pace := crawlpace.HostState{NextDueAt: time.Now().Add(time.Minute), Generation: 1}
	err := checkpoint.CompletePage(testContext, provenance, current.URL, PageCompletion{
		Tally: yagocrawlcontract.CrawlRunTally{Failed: 1},
		HostProgress: &PageHostProgress{
			Host:        current.Host,
			Progress:    HostProgress{Retired: true, Pace: pace, PaceCapacity: 8},
			DroppedURLs: []string{other.URL},
		},
	})
	if !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("mismatched atomic host error = %v", err)
	}
	snapshot, err := checkpoint.Load(testContext, provenance)
	if err != nil {
		t.Fatalf("load rolled back host completion: %v", err)
	}
	if snapshot.Completed || snapshot.Counters.Pending != 2 || len(snapshot.Outstanding) != 2 ||
		snapshot.HostStates[current.Host].Retired || snapshot.Tally != (yagocrawlcontract.CrawlRunTally{}) {
		t.Fatalf("rolled back host completion snapshot = %+v", snapshot)
	}
	paces, err := checkpoint.HostPaces(testContext, 8)
	if err != nil || len(paces) != 0 {
		t.Fatalf("rolled back host paces = %+v, %v", paces, err)
	}
}

func TestPageCompletionRejectsInvalidHostProgress(t *testing.T) {
	checkpoint, provenance, page := admittedCheckpoint(t)
	invalid := []*PageHostProgress{
		{Progress: HostProgress{}},
		{Host: page.Host, Progress: HostProgress{PaceCapacity: -1}},
		{Host: page.Host, DroppedURLs: []string{" "}},
	}
	for index, hostProgress := range invalid {
		if err := checkpoint.CompletePage(testContext, provenance, page.URL, PageCompletion{
			HostProgress: hostProgress,
		}); err == nil {
			t.Fatalf("invalid page host progress %d succeeded", index)
		}
	}
}

func TestHostOutcomeGenerationRejectsReverseCompletion(t *testing.T) {
	now := time.Date(2026, 7, 17, 4, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		olderPace crawlpace.HostState
		newerPace crawlpace.HostState
	}{
		{
			name:      "different pace generations",
			olderPace: crawlpace.HostState{NextDueAt: now.Add(time.Second), Generation: 1},
			newerPace: crawlpace.HostState{NextDueAt: now.Add(2 * time.Second), Generation: 2},
		},
		{
			name:      "equal pace generation",
			olderPace: crawlpace.HostState{NextDueAt: now.Add(2 * time.Second), Generation: 2},
			newerPace: crawlpace.HostState{NextDueAt: now.Add(2 * time.Second), Generation: 2},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			verifyReverseHostCompletion(t, testCase.olderPace, testCase.newerPace)
		})
	}
}

func verifyReverseHostCompletion(
	t *testing.T,
	olderPace crawlpace.HostState,
	newerPace crawlpace.HostState,
) {
	t.Helper()
	path := testCheckpointPath(t)
	checkpoint := openTestCheckpoint(t, path)
	provenance := []byte("host-generation")
	beginTestRun(t, checkpoint, provenance, []byte("host-generation-order"))
	older := testPage("https://busy.example/older", "busy.example", "older", 0)
	newer := testPage("https://busy.example/newer", "busy.example", "newer", 0)
	dropped := testPage("https://busy.example/dropped", "busy.example", "dropped", 0)
	if admitted, err := checkpoint.Admit(
		testContext,
		provenance,
		[]Page{older, newer, dropped},
	); err != nil || admitted != 3 {
		t.Fatalf("admit generation pages = %d, %v", admitted, err)
	}
	if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
		t.Fatalf("finish generation seeding: %v", err)
	}
	fixture := hostGenerationFixture{
		checkpoint: checkpoint,
		provenance: provenance,
		older:      older,
		newer:      newer,
		dropped:    dropped,
	}
	completeNewerHostGeneration(t, fixture, newerPace)
	completeOlderHostGeneration(t, fixture, olderPace)
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close generation checkpoint: %v", err)
	}
	checkpoint = openTestCheckpoint(t, path)
	snapshot, err := checkpoint.Load(testContext, provenance)
	if err != nil {
		t.Fatalf("load generation checkpoint: %v", err)
	}
	host := snapshot.HostStates[older.Host]
	if !snapshot.Completed || snapshot.Counters.Pending != 0 ||
		host.Generation != 2 || !host.Retired || host.Failures != 0 {
		t.Fatalf("reverse completion snapshot = %+v", snapshot)
	}
	paces, err := checkpoint.HostPaces(testContext, 8)
	if err != nil || paces[older.Host] != newerPace {
		t.Fatalf("reverse completion pace = %+v, %v", paces, err)
	}
}

func completeNewerHostGeneration(
	t *testing.T,
	fixture hostGenerationFixture,
	pace crawlpace.HostState,
) {
	t.Helper()
	err := fixture.checkpoint.CompletePage(
		testContext,
		fixture.provenance,
		fixture.newer.URL,
		PageCompletion{
			HostProgress: &PageHostProgress{
				Host: fixture.newer.Host,
				Progress: HostProgress{
					Generation:   2,
					Retired:      true,
					Pace:         pace,
					PaceCapacity: 8,
				},
				DroppedURLs: []string{fixture.dropped.URL},
			},
		},
	)
	if err != nil {
		t.Fatalf("complete newer retirement: %v", err)
	}
}

func completeOlderHostGeneration(
	t *testing.T,
	fixture hostGenerationFixture,
	pace crawlpace.HostState,
) {
	t.Helper()
	err := fixture.checkpoint.CompletePage(
		testContext,
		fixture.provenance,
		fixture.older.URL,
		PageCompletion{
			HostProgress: &PageHostProgress{
				Host: fixture.older.Host,
				Progress: HostProgress{
					Generation:   1,
					Failures:     1,
					Pace:         pace,
					PaceCapacity: 8,
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("complete stale failure: %v", err)
	}
}

func TestHostOutcomeGenerationRejectsEqualSemanticConflict(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("host-generation-conflict")
	beginTestRun(t, checkpoint, provenance, []byte("host-generation-conflict-order"))
	if err := checkpoint.RecordHostState(
		testContext,
		provenance,
		"example.com",
		HostProgress{Generation: 3, Failures: 1},
		nil,
	); err != nil {
		t.Fatalf("record initial host generation: %v", err)
	}
	if err := checkpoint.RecordHostState(
		testContext,
		provenance,
		"example.com",
		HostProgress{Generation: 3, Retired: true},
		nil,
	); !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("equal host generation conflict error = %v", err)
	}
}
