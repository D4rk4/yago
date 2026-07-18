package frontier

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestPersistentHighFanoutRunsKeepNewAdmissionsOnDisk(t *testing.T) {
	const (
		runs        = 8
		pagesPerRun = 1_024
	)
	checkpoint := openBoundedRecoveryCheckpoint(t)
	fixture := persistentHighFanoutFixture{
		checkpoint:    checkpoint,
		profile:       internalProfile(t),
		crawlFrontier: NewFrontier(32, nil, WithCheckpoint(checkpoint)),
		pagesPerRun:   pagesPerRun,
	}
	runIDs := make([]uuid.UUID, 0, runs)
	for runIndex := range runs {
		runIDs = append(runIDs, fixture.admitRun(t, runIndex))
	}
	for _, runID := range runIDs {
		fixture.assertHotState(t, runID)
	}
	fixture.crawlFrontier.mu.Lock()
	ready := len(fixture.crawlFrontier.state.ready)
	maximumReady := fixture.crawlFrontier.maxReady
	fixture.crawlFrontier.mu.Unlock()
	if ready > maximumReady {
		t.Fatalf("global ready pages = %d, max %d", ready, maximumReady)
	}
}

type persistentHighFanoutFixture struct {
	checkpoint    *frontiercheckpoint.FrontierCheckpoint
	profile       crawladmission.AdmissionProfile
	crawlFrontier *Frontier
	pagesPerRun   int
}

func (fixture persistentHighFanoutFixture) admitRun(t *testing.T, runIndex int) uuid.UUID {
	t.Helper()
	provenance := []byte(fmt.Sprintf("steady-state-%02d", runIndex))
	identity := []byte(fmt.Sprintf("steady-state-order-%02d", runIndex))
	seeded := fixture.crawlFrontier.SeedRunWithPriority(
		context.Background(),
		CrawlRunSeed{
			Requests: internalRequests(
				fixture.profile,
				fmt.Sprintf("https://steady-root-%02d.example/", runIndex),
			),
			Provenance:    provenance,
			OrderIdentity: identity,
		},
		fixture.profile,
		func(bool) { t.Error("high-fanout run unexpectedly settled") },
	)
	held := internalReceive(t, fixture.crawlFrontier)
	links := make([]string, 0, fixture.pagesPerRun)
	for page := range fixture.pagesPerRun {
		links = append(
			links,
			fmt.Sprintf("https://steady-%02d-%04d.example/page", runIndex, page),
		)
	}
	if duplicates := fixture.crawlFrontier.Submit(
		context.Background(),
		held,
		crawljob.DiscoveredLinks{Followable: links},
	); duplicates != 0 {
		t.Fatalf("run %d duplicates = %d", runIndex, duplicates)
	}
	durablePages := uint64(len(links) + 1)
	assertColdPersistentAdmissionState(
		t,
		fixture.crawlFrontier,
		seeded.RunID,
		durablePages,
	)
	state, err := fixture.checkpoint.Inspect(context.Background(), provenance, identity)
	if err != nil || state.Pages != durablePages || state.Pending != durablePages {
		t.Fatalf("run %d durable state = %+v, %v", runIndex, state, err)
	}

	return seeded.RunID
}

func (fixture persistentHighFanoutFixture) assertHotState(t *testing.T, runID uuid.UUID) {
	t.Helper()
	fixture.crawlFrontier.mu.Lock()
	run := fixture.crawlFrontier.state.runs[runID]
	live := run.pendingPages + fixture.crawlFrontier.readyPerRun[runID]
	resident := residentHostReferenceTotal(run)
	hosts := len(run.hostPages)
	generations := len(run.hostGenerations)
	failures := len(run.hostFailures)
	retired := len(run.retiredHosts)
	redirects := len(run.redirects)
	progress := len(run.pageHostProgress)
	visited := len(run.visited)
	pendingHosts := len(run.pendingByHost)
	pendingSlots := len(run.pendingHosts)
	fixture.crawlFrontier.mu.Unlock()
	if live > frontiercheckpoint.RecoveryPageBatchSize || resident != live+1 ||
		hosts != resident || generations != resident || failures != 0 || retired != 0 ||
		redirects != 0 || progress != 0 || visited != 0 || pendingHosts > live ||
		pendingSlots > frontiercheckpoint.RecoveryPageBatchSize+frontierMutationBatchSize {
		t.Fatalf(
			"run %s hot state live=%d resident=%d hosts=%d generations=%d failures=%d retired=%d redirects=%d progress=%d visited=%d pendingHosts=%d/%d",
			runID,
			live,
			resident,
			hosts,
			generations,
			failures,
			retired,
			redirects,
			progress,
			visited,
			pendingHosts,
			pendingSlots,
		)
	}
}

func TestPersistentRecoveryEvictsCompletedHostWindows(t *testing.T) {
	checkpoint := openBoundedRecoveryCheckpoint(t)
	profile := internalProfile(t)
	provenance := []byte("steady-state-refill")
	identity := []byte("steady-state-refill-order")
	persistBoundedRecoveryPages(t, boundedRecoveryPersistence{
		checkpoint: checkpoint, provenance: provenance, identity: identity,
		profileHandle: profile.Profile.Handle, run: 7, total: 1_025,
	})
	crawlFrontier := NewFrontier(32, nil, WithCheckpoint(checkpoint))
	seeded := crawlFrontier.SeedRunWithPriority(
		context.Background(),
		CrawlRunSeed{Provenance: provenance, OrderIdentity: identity},
		profile,
		func(bool) { t.Error("refill eviction run unexpectedly settled") },
	)
	for range 700 {
		job := internalReceive(t, crawlFrontier)
		crawlFrontier.Done(job, successfulPageOutcome())
	}
	crawlFrontier.mu.Lock()
	run := crawlFrontier.state.runs[seeded.RunID]
	live := run.pendingPages + crawlFrontier.readyPerRun[seeded.RunID]
	resident := residentHostReferenceTotal(run)
	hosts := len(run.hostPages)
	generations := len(run.hostGenerations)
	failures := len(run.hostFailures)
	retired := len(run.retiredHosts)
	redirects := len(run.redirects)
	progress := len(run.pageHostProgress)
	visited := len(run.visited)
	pendingHosts := len(run.pendingByHost)
	pendingSlots := len(run.pendingHosts)
	cursor := run.recoveryCursor
	crawlFrontier.mu.Unlock()
	if live > frontiercheckpoint.RecoveryPageBatchSize || resident != live || hosts != live ||
		generations != live || failures != 0 || retired != 0 || redirects != 0 ||
		progress != 0 || visited != 0 || pendingHosts > live ||
		pendingSlots > frontiercheckpoint.RecoveryPageBatchSize+frontierMutationBatchSize ||
		cursor <= frontiercheckpoint.RecoveryPageBatchSize*2 {
		t.Fatalf(
			"refill state live=%d resident=%d hosts=%d generations=%d visited=%d cursor=%d",
			live,
			resident,
			hosts,
			generations,
			visited,
			cursor,
		)
	}
}

func TestNonpersistentHighFanoutRetainsLiveFrontierState(t *testing.T) {
	profile := internalProfile(t)
	crawlFrontier := NewFrontier(4, nil)
	seeded := crawlFrontier.SeedRun(
		context.Background(),
		internalRequests(profile, "https://memory-root.example/"),
		nil,
		profile,
		func(bool) { t.Error("nonpersistent run unexpectedly settled") },
	)
	held := internalReceive(t, crawlFrontier)
	const discovered = 512
	links := make([]string, 0, discovered)
	for page := range discovered {
		links = append(links, fmt.Sprintf("https://memory-%04d.example/page", page))
	}
	if duplicates := crawlFrontier.Submit(
		context.Background(),
		held,
		crawljob.DiscoveredLinks{Followable: links},
	); duplicates != 0 {
		t.Fatalf("nonpersistent duplicates = %d", duplicates)
	}
	crawlFrontier.mu.Lock()
	run := crawlFrontier.state.runs[seeded.RunID]
	visited := len(run.visited)
	hosts := len(run.hostPages)
	live := run.pendingPages + crawlFrontier.readyPerRun[seeded.RunID]
	crawlFrontier.mu.Unlock()
	if visited != discovered+1 || hosts != discovered+1 || live != discovered {
		t.Fatalf("nonpersistent state visited=%d hosts=%d live=%d", visited, hosts, live)
	}
}

func TestPersistentAdmissionUsesDurableHostBudget(t *testing.T) {
	checkpoint := openBoundedRecoveryCheckpoint(t)
	profile := boundedHostBudgetProfile(t, 3)
	provenance := []byte("durable-host-budget")
	identity := []byte("durable-host-budget-order")
	crawlFrontier := NewFrontier(4, nil, WithCheckpoint(checkpoint))
	seeded := crawlFrontier.SeedRunWithPriority(
		context.Background(),
		CrawlRunSeed{
			Requests:   internalRequests(profile, "https://budget.example/root"),
			Provenance: provenance, OrderIdentity: identity,
		},
		profile,
		func(bool) { t.Error("host-budget run unexpectedly settled") },
	)
	held := internalReceive(t, crawlFrontier)
	links := crawljob.DiscoveredLinks{Followable: []string{
		"https://budget.example/one",
		"https://budget.example/two",
		"https://budget.example/three",
		"https://budget.example/four",
	}}
	if duplicates := crawlFrontier.Submit(context.Background(), held, links); duplicates != 0 {
		t.Fatalf("host-budget duplicates = %d", duplicates)
	}
	if duplicates := crawlFrontier.Submit(
		context.Background(),
		held,
		crawljob.DiscoveredLinks{Followable: []string{
			"https://budget.example/one",
			"https://budget.example/five",
		}},
	); duplicates != 1 {
		t.Fatalf("durable duplicate total = %d, want 1", duplicates)
	}
	state, err := checkpoint.Inspect(context.Background(), provenance, identity)
	if err != nil || state.Pages != 3 || state.Pending != 3 {
		t.Fatalf("host-budget state = %+v, %v", state, err)
	}
	assertColdPersistentAdmissionState(t, crawlFrontier, seeded.RunID, 3)
}

func TestPersistentAdmissionUsesDurableRunBudget(t *testing.T) {
	checkpoint := openBoundedRecoveryCheckpoint(t)
	profile := internalProfile(t)
	provenance := []byte("durable-run-budget")
	identity := []byte("durable-run-budget-order")
	crawlFrontier := NewFrontier(
		4,
		nil,
		WithCheckpoint(checkpoint),
		WithMaxPagesPerRun(3),
	)
	seeded := crawlFrontier.SeedRunWithPriority(
		context.Background(),
		CrawlRunSeed{
			Requests:   internalRequests(profile, "https://run-budget-root.example/"),
			Provenance: provenance, OrderIdentity: identity,
		},
		profile,
		func(bool) { t.Error("run-budget run unexpectedly settled") },
	)
	held := internalReceive(t, crawlFrontier)
	if duplicates := crawlFrontier.Submit(
		context.Background(),
		held,
		crawljob.DiscoveredLinks{Followable: []string{
			"https://run-budget-one.example/",
			"https://run-budget-two.example/",
			"https://run-budget-three.example/",
		}},
	); duplicates != 0 {
		t.Fatalf("run-budget duplicates = %d", duplicates)
	}
	state, err := checkpoint.Inspect(context.Background(), provenance, identity)
	if err != nil || state.Pages != 3 || state.Pending != 3 {
		t.Fatalf("run-budget state = %+v, %v", state, err)
	}
	assertColdPersistentAdmissionState(t, crawlFrontier, seeded.RunID, 3)
}

func TestPersistentAdmissionUsesDurableHostRetirement(t *testing.T) {
	checkpoint := openBoundedRecoveryCheckpoint(t)
	profile := internalProfile(t)
	provenance := []byte("durable-retired-host")
	identity := []byte("durable-retired-host-order")
	crawlFrontier := NewFrontier(4, nil, WithCheckpoint(checkpoint))
	seeded := crawlFrontier.SeedRunWithPriority(
		context.Background(),
		CrawlRunSeed{
			Requests:   internalRequests(profile, "https://retired-root.example/"),
			Provenance: provenance, OrderIdentity: identity,
		},
		profile,
		func(bool) { t.Error("retired-host run unexpectedly settled") },
	)
	held := internalReceive(t, crawlFrontier)
	if err := checkpoint.RecordHostState(
		context.Background(),
		provenance,
		"retired-cold.example",
		frontiercheckpoint.HostProgress{Generation: 1, Retired: true},
		nil,
	); err != nil {
		t.Fatalf("retire cold host: %v", err)
	}
	if duplicates := crawlFrontier.Submit(
		context.Background(),
		held,
		crawljob.DiscoveredLinks{Followable: []string{
			"https://retired-cold.example/page",
		}},
	); duplicates != 0 {
		t.Fatalf("retired-host duplicates = %d", duplicates)
	}
	state, err := checkpoint.Inspect(context.Background(), provenance, identity)
	if err != nil || state.Pages != 1 || state.Pending != 1 {
		t.Fatalf("retired-host state = %+v, %v", state, err)
	}
	assertColdPersistentAdmissionState(t, crawlFrontier, seeded.RunID, 1)
}

func TestPersistentAdmissionRejectsCancelledRun(t *testing.T) {
	checkpoint := openBoundedRecoveryCheckpoint(t)
	profile := internalProfile(t)
	provenance := []byte("durable-cancelled-admission")
	identity := []byte("durable-cancelled-admission-order")
	crawlFrontier := NewFrontier(4, nil, WithCheckpoint(checkpoint))
	seeded := crawlFrontier.SeedRunWithPriority(
		context.Background(),
		CrawlRunSeed{
			Requests:   internalRequests(profile, "https://cancelled-root.example/"),
			Provenance: provenance, OrderIdentity: identity,
		},
		profile,
		func(bool) { t.Error("cancelled admission run unexpectedly settled") },
	)
	held := internalReceive(t, crawlFrontier)
	if !crawlFrontier.CancelControl(provenance) {
		t.Fatal("cancel persistent admission run")
	}
	if duplicates := crawlFrontier.Submit(
		context.Background(),
		held,
		crawljob.DiscoveredLinks{Followable: []string{
			"https://cancelled-child.example/",
		}},
	); duplicates != 0 {
		t.Fatalf("cancelled admission duplicates = %d", duplicates)
	}
	state, err := checkpoint.Inspect(context.Background(), provenance, identity)
	if err != nil || state.Pages != 1 || state.Pending != 1 {
		t.Fatalf("cancelled admission state = %+v, %v", state, err)
	}
	assertColdPersistentAdmissionState(t, crawlFrontier, seeded.RunID, 1)
}

func TestPersistentSeedAdmissionDeduplicatesSameBatch(t *testing.T) {
	checkpoint := openBoundedRecoveryCheckpoint(t)
	profile := internalProfile(t)
	provenance := []byte("durable-batch-duplicate")
	identity := []byte("durable-batch-duplicate-order")
	crawlFrontier := NewFrontier(4, nil, WithCheckpoint(checkpoint))
	seeded := crawlFrontier.SeedRunWithPriority(
		context.Background(),
		CrawlRunSeed{
			Requests: internalRequests(
				profile,
				"https://batch-duplicate.example/page",
				"https://batch-duplicate.example/page",
			),
			Provenance: provenance, OrderIdentity: identity,
		},
		profile,
		func(bool) { t.Error("batch-duplicate run unexpectedly settled") },
	)
	if seeded.Queued != 1 {
		t.Fatalf("batch-duplicate queued = %d, want 1", seeded.Queued)
	}
	state, err := checkpoint.Inspect(context.Background(), provenance, identity)
	if err != nil || state.Pages != 1 || state.Pending != 1 || state.Tally.Duplicates != 1 {
		t.Fatalf("batch-duplicate state = %+v, %v", state, err)
	}
}

func TestBoundedHostRetirementReleasesReturnedAndQueuedResidentPages(t *testing.T) {
	checkpoint := openBoundedRecoveryCheckpoint(t)
	profile := internalProfile(t)
	provenance := []byte("bounded-resident-retirement")
	identity := []byte("bounded-resident-retirement-order")
	persistBoundedRecoveryPages(t, boundedRecoveryPersistence{
		checkpoint: checkpoint, provenance: provenance, identity: identity,
		profileHandle: profile.Profile.Handle, total: 5,
	})
	if err := checkpoint.RecordHostState(
		context.Background(),
		provenance,
		boundedRecoveryHost(0, 0),
		frontiercheckpoint.HostProgress{Generation: 4, Failures: 4},
		nil,
	); err != nil {
		t.Fatalf("persist pre-retirement failures: %v", err)
	}
	finished := make(chan bool, 1)
	crawlFrontier := NewFrontier(2, nil, WithCheckpoint(checkpoint))
	seeded := crawlFrontier.SeedRunWithPriority(
		context.Background(),
		CrawlRunSeed{Provenance: provenance, OrderIdentity: identity},
		profile,
		func(succeeded bool) { finished <- succeeded },
	)
	held := internalReceive(t, crawlFrontier)
	crawlFrontier.RecordHostFetchOutcome(context.Background(), held, true)
	if duplicates := crawlFrontier.Submit(
		context.Background(),
		held,
		crawljob.DiscoveredLinks{Followable: []string{
			fmt.Sprintf("https://%s/rejected", boundedRecoveryHost(0, 0)),
		}},
	); duplicates != 0 {
		t.Fatalf("retired resident admission duplicates = %d", duplicates)
	}
	crawlFrontier.mu.Lock()
	run := crawlFrontier.state.runs[seeded.RunID]
	pending := run.pendingPages
	ready := crawlFrontier.readyPerRun[seeded.RunID]
	resident := residentHostReferenceTotal(run)
	_, retired := run.retiredHosts[boundedRecoveryHost(0, 0)]
	crawlFrontier.mu.Unlock()
	if pending != 0 || ready != 0 || resident != 1 || !retired ||
		crawlFrontier.RunPending(seeded.RunID) != 1 {
		t.Fatalf(
			"retired resident state pending=%d ready=%d resident=%d retired=%t outstanding=%d",
			pending,
			ready,
			resident,
			retired,
			crawlFrontier.RunPending(seeded.RunID),
		)
	}
	crawlFrontier.Done(held, successfulPageOutcome())
	select {
	case succeeded := <-finished:
		if !succeeded {
			t.Fatal("bounded resident retirement failed")
		}
	case <-time.After(time.Second):
		t.Fatal("bounded resident retirement did not settle")
	}
}

func boundedHostBudgetProfile(
	t *testing.T,
	maximumPages int,
) crawladmission.AdmissionProfile {
	t.Helper()
	profile, err := crawladmission.CompileProfile(yagocrawlcontract.NewCrawlProfile(
		yagocrawlcontract.CrawlProfile{
			Scope:           yagocrawlcontract.ScopeWide,
			URLMustMatch:    yagocrawlcontract.MatchAll,
			MaxDepth:        2,
			MaxPagesPerHost: maximumPages,
		},
	))
	if err != nil {
		t.Fatalf("compile host-budget profile: %v", err)
	}

	return profile
}

func assertColdPersistentAdmissionState(
	t *testing.T,
	crawlFrontier *Frontier,
	runID uuid.UUID,
	durablePages uint64,
) {
	t.Helper()
	crawlFrontier.mu.Lock()
	run := crawlFrontier.state.runs[runID]
	visited := len(run.visited)
	hosts := len(run.hostPages)
	generations := len(run.hostGenerations)
	failures := len(run.hostFailures)
	retired := len(run.retiredHosts)
	redirects := len(run.redirects)
	progress := len(run.pageHostProgress)
	resident := residentHostReferenceTotal(run)
	live := run.pendingPages + crawlFrontier.readyPerRun[runID]
	upper := run.recoveryUpper
	cursor := run.recoveryCursor
	complete := run.recoveryComplete
	crawlFrontier.mu.Unlock()
	wantComplete := durablePages == 1
	if visited != 0 || hosts != 1 || generations != 1 || failures != 0 || retired != 0 ||
		redirects != 0 || progress != 0 || resident != 1 || live != 0 ||
		upper != durablePages || cursor != 1 || complete != wantComplete {
		t.Fatalf(
			"cold state visited=%d hosts=%d generations=%d failures=%d retired=%d redirects=%d progress=%d resident=%d live=%d cursor=%d/%d complete=%t",
			visited,
			hosts,
			generations,
			failures,
			retired,
			redirects,
			progress,
			resident,
			live,
			cursor,
			upper,
			complete,
		)
	}
}

func residentHostReferenceTotal(run *crawlRun) int {
	total := 0
	for _, references := range run.residentHostReferences {
		total += references
	}

	return total
}
