package frontier_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yago-crawler/internal/crawldelay"
	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yago-crawler/internal/runtally"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestCheckpointCommitsOnlyEachCompletedPageOutcomeAcrossCrash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier-v1.db")
	firstCheckpoint := openRestartCheckpoint(t, path, "first")
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        0,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	provenance := []byte("outcome-crash-cut")
	identity := []byte("outcome-crash-identity")
	firstTally := runtally.New()
	firstFrontier := frontier.NewFrontier(
		2,
		nil,
		frontier.WithCheckpoint(firstCheckpoint),
		frontier.WithRunTally(firstTally),
	)
	firstFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{
			Requests: requestsFor(
				profile.Profile.Handle,
				"https://example.com/first",
				"https://example.com/second",
			),
			Provenance:    provenance,
			OrderIdentity: identity,
		},
		profile,
		func(bool) { t.Error("first frontier unexpectedly settled") },
	)
	completed := receiveJob(t, firstFrontier)
	held := receiveJob(t, firstFrontier)
	pageOutcome := yagocrawlcontract.CrawlRunTally{Fetched: 1, Indexed: 1}
	firstFrontier.Done(completed, pageOutcome)
	if got := firstTally.Snapshot(provenance); got != pageOutcome {
		t.Fatalf("pre-crash tally = %+v, want %+v", got, pageOutcome)
	}
	closeRestartCheckpoint(t, firstCheckpoint, "first")

	secondCheckpoint := openRestartCheckpoint(t, path, "recovered")
	t.Cleanup(func() { _ = secondCheckpoint.Close() })
	secondTally := runtally.New()
	secondFrontier := frontier.NewFrontier(
		2,
		nil,
		frontier.WithCheckpoint(secondCheckpoint),
		frontier.WithRunTally(secondTally),
	)
	finished := make(chan bool, 1)
	seeded := secondFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{Provenance: provenance, OrderIdentity: identity},
		profile,
		func(succeeded bool) { finished <- succeeded },
	)
	if seeded.Queued != 1 {
		t.Fatalf("recovered queued = %d, want one uncommitted page", seeded.Queued)
	}
	if got := secondTally.Snapshot(provenance); got != pageOutcome {
		t.Fatalf("recovered tally = %+v, want only committed page %+v", got, pageOutcome)
	}
	replayed := receiveJob(t, secondFrontier)
	if replayed.URL != held.URL || replayed.ObservationID != held.ObservationID {
		t.Fatalf("replayed page = %+v, want held page %+v", replayed, held)
	}
	secondFrontier.Done(replayed, pageOutcome)
	expectSuccessfulSettlement(t, finished, "recovered run")
	want := yagocrawlcontract.CrawlRunTally{Fetched: 2, Indexed: 2}
	if got := secondTally.Snapshot(provenance); got != want {
		t.Fatalf("final tally = %+v, want %+v", got, want)
	}
}

func TestCheckpointRecoveryKeepsProfileRunBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier-v1.db")
	firstCheckpoint := openRestartCheckpoint(t, path, "first budget")
	maximum := 3
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        1,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
		MaxPagesPerRun:  &maximum,
	})
	provenance := []byte("profile-budget-restart")
	identity := []byte("profile-budget-order")
	firstFrontier := frontier.NewFrontier(
		3,
		nil,
		frontier.WithCheckpoint(firstCheckpoint),
		frontier.WithMaxPagesPerRun(1),
	)
	firstFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{
			Requests:      requestsFor(profile.Profile.Handle, "https://example.com/root"),
			Provenance:    provenance,
			OrderIdentity: identity,
		},
		profile,
		func(bool) { t.Error("first frontier unexpectedly settled") },
	)
	recoveredRoot := receiveJob(t, firstFrontier)
	closeRestartCheckpoint(t, firstCheckpoint, "first budget")

	secondCheckpoint := openRestartCheckpoint(t, path, "second budget")
	t.Cleanup(func() { _ = secondCheckpoint.Close() })
	secondFrontier := frontier.NewFrontier(
		3,
		nil,
		frontier.WithCheckpoint(secondCheckpoint),
		frontier.WithMaxPagesPerRun(1),
	)
	finished := make(chan bool, 1)
	seeded := secondFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{Provenance: provenance, OrderIdentity: identity},
		profile,
		func(succeeded bool) { finished <- succeeded },
	)
	if seeded.Queued != 1 {
		t.Fatalf("recovered queued = %d, want 1", seeded.Queued)
	}
	root := receiveJob(t, secondFrontier)
	if root.URL != recoveredRoot.URL {
		t.Fatalf("recovered root = %q, want %q", root.URL, recoveredRoot.URL)
	}
	duplicates := secondFrontier.Submit(
		context.Background(),
		root,
		discoveredLinks("https://example.com/one", "https://example.com/two"),
	)
	if duplicates != 0 {
		t.Fatalf("duplicates = %d, want 0", duplicates)
	}
	secondFrontier.Done(root, successfulPageOutcome())
	for range 2 {
		secondFrontier.Done(receiveJob(t, secondFrontier), successfulPageOutcome())
	}
	expectSuccessfulSettlement(t, finished, "profile-budget run")
}

func TestCheckpointRecoveryDoesNotBurstPastRestoredRunRate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier-v1.db")
	firstCheckpoint, err := frontiercheckpoint.Open(path)
	if err != nil {
		t.Fatalf("open first checkpoint: %v", err)
	}
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        0,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	provenance := []byte("rate-restart")
	identity := []byte("rate-restart-order")
	firstFrontier := frontier.NewFrontier(2, nil, frontier.WithCheckpoint(firstCheckpoint))
	firstFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{
			Requests: requestsFor(
				profile.Profile.Handle,
				"https://a.example/first",
				"https://b.example/second",
			),
			Provenance:    provenance,
			OrderIdentity: identity,
		},
		profile,
		func(bool) { t.Error("first frontier unexpectedly settled") },
	)
	if !firstFrontier.SetRateControl(provenance, 1) {
		t.Fatal("persist one-page-per-minute rate")
	}
	if err := firstCheckpoint.Close(); err != nil {
		t.Fatalf("close first checkpoint: %v", err)
	}

	secondCheckpoint, err := frontiercheckpoint.Open(path)
	if err != nil {
		t.Fatalf("reopen checkpoint: %v", err)
	}
	t.Cleanup(func() { _ = secondCheckpoint.Close() })
	secondFrontier := frontier.NewFrontier(2, nil, frontier.WithCheckpoint(secondCheckpoint))
	seeded := secondFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{Provenance: provenance, OrderIdentity: identity},
		profile,
		func(bool) {},
	)
	if seeded.Queued != 2 {
		t.Fatalf("recovered queued = %d, want 2", seeded.Queued)
	}
	assertNoJob(t, secondFrontier, 100*time.Millisecond)
	if !secondFrontier.SetRateControl(provenance, 0) {
		t.Fatal("lift recovered rate")
	}
	first := receiveJob(t, secondFrontier)
	second := receiveJob(t, secondFrontier)
	secondFrontier.Done(first, successfulPageOutcome())
	secondFrontier.Done(second, successfulPageOutcome())
}

func TestCheckpointRestoresCrossRunHostBackoffAfterSourceRunDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier-v1.db")
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        0,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	source := restartRunIdentity{
		profile:    profile,
		provenance: []byte("backoff-source"),
		identity:   []byte("backoff-source-order"),
	}
	survivor := restartRunIdentity{
		profile:    profile,
		provenance: []byte("backoff-survivor"),
		identity:   []byte("backoff-survivor-order"),
	}
	backoffStarted := persistCrossRunBackoff(t, path, source, survivor)
	assertRestoredCrossRunBackoff(t, path, survivor, backoffStarted)
}

func TestRetiredHostStillPersistsItsLatestGlobalPace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier-v1.db")
	checkpoint, err := frontiercheckpoint.Open(path)
	if err != nil {
		t.Fatalf("open checkpoint: %v", err)
	}
	t.Cleanup(func() { _ = checkpoint.Close() })
	pace, err := crawldelay.NewHostPace(time.Second, 8)
	if err != nil {
		t.Fatalf("create host pace: %v", err)
	}
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        0,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	provenance := []byte("retired-pace")
	crawlFrontier := frontier.NewFrontier(1, pace, frontier.WithCheckpoint(checkpoint))
	crawlFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{
			Requests:      requestsFor(profile.Profile.Handle, "https://retired.example/page"),
			Provenance:    provenance,
			OrderIdentity: []byte("retired-pace-order"),
		},
		profile,
		func(bool) {},
	)
	work := receiveJob(t, crawlFrontier)
	for range 5 {
		crawlFrontier.RecordHostFetchOutcome(context.Background(), work, true)
	}
	future := time.Now().Add(10 * time.Minute)
	futureState := pace.SnapshotHost(work.URL)
	futureState.NextDueAt = future
	futureState.Generation++
	pace.RestoreHost("retired.example", futureState)
	if restored := pace.SnapshotHost(work.URL); restored != futureState {
		t.Fatalf("restored retired host pace = %+v, want %+v", restored, futureState)
	}
	crawlFrontier.RecordHostFetchOutcome(context.Background(), work, true)
	crawlFrontier.Done(work, successfulPageOutcome())
	states, err := checkpoint.HostPaces(context.Background(), pace.Capacity())
	if err != nil {
		t.Fatalf("load retired host pace: %v", err)
	}
	if got := states["retired.example"].NextDueAt; !got.Equal(future) {
		t.Fatalf("retired host pace = %v, want %v", got, future)
	}
}

func TestCheckpointRestoresClaimedParentAndCommittedChild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crawler", "frontier-v1.db")
	firstCheckpoint := openRestartCheckpoint(t, path, "first")
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        1,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	provenance := []byte("restart-run")
	identity := []byte("restart-order")
	firstFrontier := frontier.NewFrontier(4, nil, frontier.WithCheckpoint(firstCheckpoint))
	firstFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{
			Requests:      requestsFor(profile.Profile.Handle, "https://example.com/parent"),
			Provenance:    provenance,
			OrderIdentity: identity,
		},
		profile,
		func(bool) { t.Error("crashed frontier unexpectedly settled") },
	)
	parent := receiveJob(t, firstFrontier)
	firstFrontier.Submit(context.Background(), parent, crawljob.DiscoveredLinks{
		Followable: []string{"https://example.com/child"},
	})
	if parent.ObservationID == "" || parent.ObservedAt.IsZero() {
		t.Fatalf("parent observation is incomplete: %#v", parent)
	}
	closeRestartCheckpoint(t, firstCheckpoint, "first")

	secondCheckpoint := openRestartCheckpoint(t, path, "recovered")
	t.Cleanup(func() { _ = secondCheckpoint.Close() })
	secondFrontier := frontier.NewFrontier(4, nil, frontier.WithCheckpoint(secondCheckpoint))
	recovery, err := secondFrontier.Recovery(context.Background(), provenance, identity)
	if err != nil || !recovery.Checkpointed || recovery.Completed || recovery.Seeding ||
		recovery.Failed {
		t.Fatalf("initial recovery = %+v, %v", recovery, err)
	}
	finished := make(chan bool, 1)
	seeded := secondFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{Provenance: provenance, OrderIdentity: identity},
		profile,
		func(succeeded bool) { finished <- succeeded },
	)
	if seeded.Queued != 2 {
		t.Fatalf("restored queued = %d, want 2", seeded.Queued)
	}
	restoredParent := receiveJob(t, secondFrontier)
	restoredChild := receiveJob(t, secondFrontier)
	assertRestoredParentAndChild(t, parent, restoredParent, restoredChild)
	secondFrontier.Done(restoredParent, successfulPageOutcome())
	secondFrontier.Done(restoredChild, successfulPageOutcome())
	expectSuccessfulSettlement(t, finished, "restored run")
	recovery, err = secondFrontier.Recovery(context.Background(), provenance, identity)
	if err != nil || !recovery.Completed || recovery.Failed {
		t.Fatalf("completed recovery = %+v, %v", recovery, err)
	}
	if err := secondFrontier.ForgetCheckpoint(context.Background(), provenance); err != nil {
		t.Fatalf("forget checkpoint: %v", err)
	}
	recovery, err = secondFrontier.Recovery(context.Background(), provenance, identity)
	if err != nil || recovery.Checkpointed {
		t.Fatalf("forgotten recovery = %+v, %v", recovery, err)
	}
}

func TestCheckpointContinuesInterruptedSeedAdmissionExactly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier-v1.db")
	checkpoint := openRestartCheckpoint(t, path, "partial seed")
	t.Cleanup(func() { _ = checkpoint.Close() })
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        0,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	provenance := []byte("partial-seed")
	identity := []byte("partial-order")
	existingObservedAt, existingPage, newPage := interruptedSeedPages(profile.Profile.Handle)
	persistInterruptedSeedPrefix(
		t,
		checkpoint,
		provenance,
		identity,
		[]frontiercheckpoint.Page{existingPage, newPage},
	)
	crawlFrontier := frontier.NewFrontier(4, nil, frontier.WithCheckpoint(checkpoint))
	seeded := crawlFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{
			Provenance:    provenance,
			OrderIdentity: identity,
		},
		profile,
		func(bool) {},
	)
	if seeded.Queued != 2 {
		t.Fatalf("queued = %d, want two exact seeds", seeded.Queued)
	}
	first := receiveJob(t, crawlFrontier)
	second := receiveJob(t, crawlFrontier)
	jobs := map[string]crawljob.CrawlJob{first.URL: first, second.URL: second}
	existing := jobs["https://example.com/existing"]
	if existing.ObservationID != "existing-observation" ||
		!existing.ObservedAt.Equal(existingObservedAt) {
		t.Fatalf("restored existing seed = %#v", existing)
	}
	if admitted := jobs["https://example.com/new"]; admitted.ObservationID != "new-observation" {
		t.Fatalf("new admitted seed = %#v", admitted)
	}
	snapshot, err := checkpoint.Load(context.Background(), provenance)
	if err != nil {
		t.Fatalf("load continued seed: %v", err)
	}
	if snapshot.Seeding || snapshot.Counters.Pages != 2 || snapshot.Counters.Pending != 2 {
		t.Fatalf("continued seed snapshot = %+v", snapshot)
	}
	crawlFrontier.Done(first, successfulPageOutcome())
	crawlFrontier.Done(second, successfulPageOutcome())
}

func TestCheckpointRestoresHostFailureCircuitAndDropsRetiredQueue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier-v1.db")
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        0,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	provenance := []byte("host-recovery")
	identity := []byte("host-order")
	run := restartRunIdentity{profile: profile, provenance: provenance, identity: identity}
	persistHostFailureCircuit(t, path, run)

	secondCheckpoint := openRestartCheckpoint(t, path, "retired host")
	t.Cleanup(func() { _ = secondCheckpoint.Close() })
	secondFrontier := frontier.NewFrontier(4, nil, frontier.WithCheckpoint(secondCheckpoint))
	finished := make(chan bool, 1)
	seeded := secondFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{Provenance: provenance, OrderIdentity: identity},
		profile,
		func(succeeded bool) { finished <- succeeded },
	)
	claimed := receiveJob(t, secondFrontier)
	secondFrontier.RecordHostFetchOutcome(context.Background(), claimed, true)
	if pending := secondFrontier.RunPending(seeded.RunID); pending != 1 {
		t.Fatalf("pending after retirement = %d, want claimed page only", pending)
	}
	secondFrontier.Done(claimed, successfulPageOutcome())
	expectSuccessfulSettlement(t, finished, "retired-host run")
	snapshot, err := secondCheckpoint.Load(context.Background(), provenance)
	if err != nil {
		t.Fatalf("load retired run: %v", err)
	}
	host := snapshot.HostStates["failed.example"]
	if !snapshot.Completed || !host.Retired || host.Failures != 0 ||
		len(snapshot.Outstanding) != 0 {
		t.Fatalf("retired snapshot = %+v", snapshot)
	}
}

func TestCheckpointReplaysReservedRedirectAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier-v1.db")
	firstCheckpoint, err := frontiercheckpoint.Open(path)
	if err != nil {
		t.Fatalf("open first checkpoint: %v", err)
	}
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        0,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	provenance := []byte("redirect-recovery")
	identity := []byte("redirect-order")
	firstFrontier := frontier.NewFrontier(2, nil, frontier.WithCheckpoint(firstCheckpoint))
	firstFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{
			Requests:      requestsFor(profile.Profile.Handle, "https://source.example/start"),
			Provenance:    provenance,
			OrderIdentity: identity,
		},
		profile,
		func(bool) { t.Error("crashed frontier unexpectedly settled") },
	)
	work := receiveJob(t, firstFrontier)
	finalURL := "https://final.example/page"
	if !firstFrontier.ResolveRedirect(work, finalURL) {
		t.Fatal("fresh redirect was rejected")
	}
	reserved, err := firstCheckpoint.Load(context.Background(), provenance)
	if err != nil {
		t.Fatalf("load reserved redirect: %v", err)
	}
	if len(reserved.Outstanding) != 1 || reserved.Outstanding[0].RedirectURL != finalURL ||
		reserved.HostStates["final.example"].Pages != 1 {
		t.Fatalf("reserved redirect snapshot = %+v", reserved)
	}
	if err := firstCheckpoint.Close(); err != nil {
		t.Fatalf("close first checkpoint: %v", err)
	}

	secondCheckpoint, err := frontiercheckpoint.Open(path)
	if err != nil {
		t.Fatalf("reopen checkpoint: %v", err)
	}
	t.Cleanup(func() { _ = secondCheckpoint.Close() })
	secondFrontier := frontier.NewFrontier(2, nil, frontier.WithCheckpoint(secondCheckpoint))
	finished := make(chan bool, 1)
	secondFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{Provenance: provenance, OrderIdentity: identity},
		profile,
		func(succeeded bool) { finished <- succeeded },
	)
	replayed := receiveJob(t, secondFrontier)
	if replayed.URL != work.URL || replayed.ObservationID != work.ObservationID {
		t.Fatalf("replayed source = %#v, want %#v", replayed, work)
	}
	if !secondFrontier.ResolveRedirect(replayed, finalURL) {
		t.Fatal("same-source redirect replay was rejected")
	}
	afterReplay, err := secondCheckpoint.Load(context.Background(), provenance)
	if err != nil {
		t.Fatalf("load replayed redirect: %v", err)
	}
	if afterReplay.HostStates["final.example"].Pages != 1 {
		t.Fatalf("final host pages = %d, want 1", afterReplay.HostStates["final.example"].Pages)
	}
	secondFrontier.Done(replayed, successfulPageOutcome())
	select {
	case succeeded := <-finished:
		if !succeeded {
			t.Fatal("redirect replay reported failure")
		}
	case <-time.After(time.Second):
		t.Fatal("redirect replay did not settle")
	}
}

func TestCheckpointReconcilesChangedRedirectAfterRestart(t *testing.T) {
	cases := []redirectRestartCase{
		{
			name:         "retarget",
			finalURL:     "https://changed.example/page",
			admitted:     true,
			reservedURL:  "https://changed.example/page",
			reservedHost: "changed.example",
		},
		{
			name:     "direct",
			finalURL: "https://source.example/start",
			admitted: true,
		},
		{
			name:                 "independently visited target",
			finalURL:             "https://changed.example/page",
			independentlyVisited: true,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			runChangedRedirectRestartCase(t, testCase)
		})
	}
}

type restartRunIdentity struct {
	profile    crawladmission.AdmissionProfile
	provenance []byte
	identity   []byte
}

type redirectRestartCase struct {
	name                 string
	finalURL             string
	independentlyVisited bool
	admitted             bool
	reservedURL          string
	reservedHost         string
}

type redirectRestartSetup struct {
	path     string
	run      restartRunIdentity
	requests []yagocrawlcontract.CrawlRequest
	source   crawljob.CrawlJob
	oldFinal string
	testCase redirectRestartCase
}

type redirectRestartRecovery struct {
	checkpoint *frontiercheckpoint.FrontierCheckpoint
	frontier   *frontier.Frontier
	source     crawljob.CrawlJob
}

func openRestartCheckpoint(
	t *testing.T,
	path string,
	phase string,
) *frontiercheckpoint.FrontierCheckpoint {
	t.Helper()
	checkpoint, err := frontiercheckpoint.Open(path)
	if err != nil {
		t.Fatalf("open %s checkpoint: %v", phase, err)
	}

	return checkpoint
}

func closeRestartCheckpoint(
	t *testing.T,
	checkpoint *frontiercheckpoint.FrontierCheckpoint,
	phase string,
) {
	t.Helper()
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close %s checkpoint: %v", phase, err)
	}
}

func newRestartAdaptivePace(t *testing.T, phase string) *crawldelay.AdaptivePace {
	t.Helper()
	hostPace, err := crawldelay.NewHostPace(0, 8)
	if err != nil {
		t.Fatalf("create %s host pace: %v", phase, err)
	}
	pace, err := crawldelay.NewAdaptivePace(hostPace, 8, nil)
	if err != nil {
		t.Fatalf("create %s adaptive pace: %v", phase, err)
	}

	return pace
}

func expectSuccessfulSettlement(t *testing.T, finished <-chan bool, run string) {
	t.Helper()
	select {
	case succeeded := <-finished:
		if !succeeded {
			t.Fatalf("%s reported failure", run)
		}
	case <-time.After(time.Second):
		t.Fatalf("%s did not settle", run)
	}
}

func assertRestoredParentAndChild(
	t *testing.T,
	parent crawljob.CrawlJob,
	restoredParent crawljob.CrawlJob,
	restoredChild crawljob.CrawlJob,
) {
	t.Helper()
	if restoredParent.URL != parent.URL || restoredParent.ObservationID != parent.ObservationID ||
		!restoredParent.ObservedAt.Equal(parent.ObservedAt) {
		t.Fatalf("restored parent = %#v, want observation from %#v", restoredParent, parent)
	}
	if restoredChild.URL != "https://example.com/child" || restoredChild.ObservationID == "" {
		t.Fatalf("restored child = %#v", restoredChild)
	}
}

func interruptedSeedPages(
	profileHandle string,
) (time.Time, frontiercheckpoint.Page, frontiercheckpoint.Page) {
	existingObservedAt := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	existingPage := frontiercheckpoint.Page{
		URL:           "https://example.com/existing",
		Host:          "example.com",
		ProfileHandle: profileHandle,
		ObservationID: "existing-observation",
		ObservedAt:    existingObservedAt,
		Index:         true,
	}
	newPage := frontiercheckpoint.Page{
		URL:           "https://example.com/new",
		Host:          "example.com",
		ProfileHandle: profileHandle,
		ObservationID: "new-observation",
		ObservedAt:    existingObservedAt.Add(time.Minute),
		Index:         true,
	}

	return existingObservedAt, existingPage, newPage
}

func persistInterruptedSeedPrefix(
	t *testing.T,
	checkpoint *frontiercheckpoint.FrontierCheckpoint,
	provenance []byte,
	identity []byte,
	pages []frontiercheckpoint.Page,
) {
	t.Helper()
	if err := checkpoint.BeginSeedManifest(
		context.Background(),
		provenance,
		identity,
		yagocrawlcontract.CrawlOrderPriorityNormal,
		pages,
	); err != nil {
		t.Fatalf("begin partial run: %v", err)
	}
	result, err := checkpoint.AdmitSeedBatch(
		context.Background(),
		provenance,
		frontiercheckpoint.SeedBatch{
			Decisions: []frontiercheckpoint.SeedDecision{{Page: pages[0], Admit: true}},
		},
	)
	if err != nil || result.Admitted != 1 {
		t.Fatalf("admit existing seed = %+v, %v", result, err)
	}
}

func persistHostFailureCircuit(t *testing.T, path string, run restartRunIdentity) {
	t.Helper()
	checkpoint := openRestartCheckpoint(t, path, "host failure")
	crawlFrontier := frontier.NewFrontier(4, nil, frontier.WithCheckpoint(checkpoint))
	crawlFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{
			Requests:      failedHostRequests(run.profile.Profile.Handle),
			Provenance:    run.provenance,
			OrderIdentity: run.identity,
		},
		run.profile,
		func(bool) { t.Error("first frontier unexpectedly settled") },
	)
	recordFailedHostJobs(t, crawlFrontier, 4)
	recordSuccessfulHostJob(t, crawlFrontier)
	resetSnapshot, err := checkpoint.Load(context.Background(), run.provenance)
	if err != nil || resetSnapshot.HostStates["failed.example"].Failures != 0 {
		t.Fatalf("reset host failures = %+v, %v", resetSnapshot.HostStates, err)
	}
	recordFailedHostJobs(t, crawlFrontier, 4)
	snapshot, err := checkpoint.Load(context.Background(), run.provenance)
	if err != nil || snapshot.HostStates["failed.example"].Failures != 4 {
		t.Fatalf("persisted host failures = %+v, %v", snapshot.HostStates, err)
	}
	closeRestartCheckpoint(t, checkpoint, "host failure")
}

func persistCrossRunBackoff(
	t *testing.T,
	path string,
	source restartRunIdentity,
	survivor restartRunIdentity,
) time.Time {
	t.Helper()
	checkpoint := openRestartCheckpoint(t, path, "backoff source")
	pace := newRestartAdaptivePace(t, "backoff source")
	crawlFrontier := frontier.NewFrontier(2, pace, frontier.WithCheckpoint(checkpoint))
	crawlFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{
			Requests: requestsFor(
				survivor.profile.Profile.Handle,
				"https://busy.example/survivor",
			),
			Provenance:    survivor.provenance,
			OrderIdentity: survivor.identity,
		},
		survivor.profile,
		func(bool) { t.Error("surviving run unexpectedly settled") },
	)
	finished := make(chan bool, 1)
	crawlFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{
			Requests: requestsFor(
				source.profile.Profile.Handle,
				"https://busy.example/source",
			),
			Provenance:    source.provenance,
			OrderIdentity: source.identity,
		},
		source.profile,
		func(succeeded bool) { finished <- succeeded },
	)
	work := receiveJob(t, crawlFrontier)
	if string(work.Provenance) != string(source.provenance) {
		t.Fatalf("first dispatched provenance = %q", work.Provenance)
	}
	backoffStarted := time.Now()
	pace.Throttled(work.URL, 10*time.Minute, backoffStarted)
	crawlFrontier.RecordHostFetchOutcome(context.Background(), work, true)
	crawlFrontier.Done(work, successfulPageOutcome())
	expectSuccessfulSettlement(t, finished, "source run")
	if err := crawlFrontier.ForgetCheckpoint(context.Background(), source.provenance); err != nil {
		t.Fatalf("delete source checkpoint: %v", err)
	}
	closeRestartCheckpoint(t, checkpoint, "backoff source")

	return backoffStarted
}

func assertRestoredCrossRunBackoff(
	t *testing.T,
	path string,
	survivor restartRunIdentity,
	backoffStarted time.Time,
) {
	t.Helper()
	checkpoint := openRestartCheckpoint(t, path, "backoff recovery")
	t.Cleanup(func() { _ = checkpoint.Close() })
	pace := newRestartAdaptivePace(t, "backoff recovery")
	states, err := checkpoint.HostPaces(context.Background(), pace.Capacity())
	if err != nil {
		t.Fatalf("load restored host pace ledger: %v", err)
	}
	for host, state := range states {
		pace.RestoreHost(host, state)
	}
	crawlFrontier := frontier.NewFrontier(2, pace, frontier.WithCheckpoint(checkpoint))
	seeded := crawlFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{
			Provenance:    survivor.provenance,
			OrderIdentity: survivor.identity,
		},
		survivor.profile,
		func(bool) {},
	)
	if seeded.Queued != 1 {
		t.Fatalf("recovered survivor queued = %d, want 1", seeded.Queued)
	}
	assertNoJob(t, crawlFrontier, 100*time.Millisecond)
	due := pace.DueAt(crawljob.CrawlJob{URL: "https://busy.example/survivor"}, backoffStarted)
	if due.Before(backoffStarted.Add(10 * time.Minute)) {
		t.Fatalf("restored cross-run backoff = %v", due)
	}
}

func failedHostRequests(profileHandle string) []yagocrawlcontract.CrawlRequest {
	pages := []string{
		"one",
		"two",
		"three",
		"four",
		"reset",
		"five",
		"six",
		"seven",
		"eight",
		"final",
	}
	urls := make([]string, 0, len(pages))
	for _, page := range pages {
		urls = append(urls, "https://failed.example/"+page)
	}

	return requestsFor(profileHandle, urls...)
}

func recordFailedHostJobs(t *testing.T, crawlFrontier *frontier.Frontier, total int) {
	t.Helper()
	for range total {
		work := receiveJob(t, crawlFrontier)
		crawlFrontier.RecordHostFetchOutcome(context.Background(), work, true)
		crawlFrontier.Done(work, failedPageOutcome())
	}
}

func recordSuccessfulHostJob(t *testing.T, crawlFrontier *frontier.Frontier) {
	t.Helper()
	work := receiveJob(t, crawlFrontier)
	crawlFrontier.RecordHostFetchOutcome(context.Background(), work, false)
	crawlFrontier.Done(work, successfulPageOutcome())
}

func runChangedRedirectRestartCase(t *testing.T, testCase redirectRestartCase) {
	t.Helper()
	setup := persistChangedRedirectBeforeRestart(t, testCase)
	recovery := recoverChangedRedirectSource(t, setup)
	if admitted := recovery.frontier.ResolveRedirect(
		recovery.source,
		testCase.finalURL,
	); admitted != testCase.admitted {
		t.Fatalf("changed redirect admitted = %v, want %v", admitted, testCase.admitted)
	}
	snapshot, err := recovery.checkpoint.Load(context.Background(), setup.run.provenance)
	if err != nil {
		t.Fatalf("load reconciled redirect: %v", err)
	}
	assertReconciledRedirect(t, setup, snapshot)
}

func persistChangedRedirectBeforeRestart(
	t *testing.T,
	testCase redirectRestartCase,
) redirectRestartSetup {
	t.Helper()
	path := filepath.Join(t.TempDir(), "frontier-v1.db")
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeWide,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	run := restartRunIdentity{
		profile:    profile,
		provenance: []byte("redirect-change-" + testCase.name),
		identity:   []byte("redirect-change-order-" + testCase.name),
	}
	requests := requestsFor(profile.Profile.Handle, "https://source.example/start")
	if testCase.independentlyVisited {
		requests = append(requests, requestsFor(profile.Profile.Handle, testCase.finalURL)...)
	}
	checkpoint := openRestartCheckpoint(t, path, "redirect source")
	crawlFrontier := frontier.NewFrontier(2, nil, frontier.WithCheckpoint(checkpoint))
	crawlFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{
			Requests:      requests,
			Provenance:    run.provenance,
			OrderIdentity: run.identity,
		},
		profile,
		func(bool) { t.Error("crashed frontier unexpectedly settled") },
	)
	source := claimRedirectSource(t, crawlFrontier, len(requests))
	oldFinal := "https://old.example/page"
	if !crawlFrontier.ResolveRedirect(source, oldFinal) {
		t.Fatal("initial redirect was rejected")
	}
	closeRestartCheckpoint(t, checkpoint, "redirect source")

	return redirectRestartSetup{
		path:     path,
		run:      run,
		requests: requests,
		source:   source,
		oldFinal: oldFinal,
		testCase: testCase,
	}
}

func recoverChangedRedirectSource(
	t *testing.T,
	setup redirectRestartSetup,
) redirectRestartRecovery {
	t.Helper()
	checkpoint := openRestartCheckpoint(t, setup.path, "redirect recovery")
	t.Cleanup(func() { _ = checkpoint.Close() })
	crawlFrontier := frontier.NewFrontier(2, nil, frontier.WithCheckpoint(checkpoint))
	crawlFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{
			Provenance:    setup.run.provenance,
			OrderIdentity: setup.run.identity,
		},
		setup.run.profile,
		func(bool) {},
	)

	return redirectRestartRecovery{
		checkpoint: checkpoint,
		frontier:   crawlFrontier,
		source:     claimRedirectSource(t, crawlFrontier, len(setup.requests)),
	}
}

func claimRedirectSource(
	t *testing.T,
	crawlFrontier *frontier.Frontier,
	total int,
) crawljob.CrawlJob {
	t.Helper()
	var source crawljob.CrawlJob
	for range total {
		job := receiveJob(t, crawlFrontier)
		if job.URL == "https://source.example/start" {
			source = job
		} else {
			crawlFrontier.Abandon(job)
		}
	}

	return source
}

func assertReconciledRedirect(
	t *testing.T,
	setup redirectRestartSetup,
	snapshot frontiercheckpoint.Snapshot,
) {
	t.Helper()
	if _, found := snapshot.Visited[setup.oldFinal]; found {
		t.Fatalf("old redirect remained visited: %v", snapshot.Visited)
	}
	if snapshot.HostStates["old.example"].Pages != 0 {
		t.Fatalf("old redirect host state = %+v", snapshot.HostStates["old.example"])
	}
	var sourcePage frontiercheckpoint.Page
	for _, page := range snapshot.Outstanding {
		if page.URL == setup.source.URL {
			sourcePage = page
		}
	}
	if sourcePage.RedirectURL != setup.testCase.reservedURL ||
		sourcePage.RedirectHost != setup.testCase.reservedHost {
		t.Fatalf("reconciled source page = %+v", sourcePage)
	}
	reservedHost := setup.testCase.reservedHost
	if reservedHost != "" && snapshot.HostStates[reservedHost].Pages != 1 {
		t.Fatalf("new redirect host state = %+v", snapshot.HostStates[reservedHost])
	}
}
