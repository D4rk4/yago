package frontier

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestTwentyLargeRecoveredRunsKeepOnlyOneLiveWindowEach(t *testing.T) {
	checkpoint := openBoundedRecoveryCheckpoint(t)
	profile := internalProfile(t)
	const (
		runs        = 20
		pagesPerRun = 1_025
	)
	identities := make([][]byte, 0, runs)
	provenances := make([][]byte, 0, runs)
	for run := range runs {
		provenance := []byte(fmt.Sprintf("bounded-run-%02d", run))
		identity := []byte(fmt.Sprintf("bounded-order-%02d", run))
		persistBoundedRecoveryPages(t, boundedRecoveryPersistence{
			checkpoint: checkpoint, provenance: provenance, identity: identity,
			profileHandle: profile.Profile.Handle, run: run, total: pagesPerRun,
		})
		provenances = append(provenances, provenance)
		identities = append(identities, identity)
	}

	crawlFrontier := NewFrontier(64, nil, WithCheckpoint(checkpoint))
	for run := range runs {
		seeded := crawlFrontier.SeedRunWithPriority(
			context.Background(),
			CrawlRunSeed{
				Provenance:    provenances[run],
				OrderIdentity: identities[run],
			},
			profile,
			func(bool) { t.Error("large recovered run unexpectedly settled") },
		)
		if seeded.Queued != pagesPerRun {
			t.Fatalf("run %d queued = %d, want %d", run, seeded.Queued, pagesPerRun)
		}
		crawlFrontier.mu.Lock()
		recovered := crawlFrontier.state.runs[seeded.RunID]
		live := recovered.pendingPages + crawlFrontier.readyPerRun[seeded.RunID]
		visited := len(recovered.visited)
		hosts := len(recovered.hostPages)
		bounded := recovered.boundedRecovery
		complete := recovered.recoveryComplete
		crawlFrontier.mu.Unlock()
		if !bounded || complete || live > frontiercheckpoint.RecoveryPageBatchSize ||
			visited != 0 || hosts > frontiercheckpoint.RecoveryPageBatchSize {
			t.Fatalf(
				"run %d bounded=%t complete=%t live=%d visited=%d hosts=%d",
				run,
				bounded,
				complete,
				live,
				visited,
				hosts,
			)
		}
		if pending := crawlFrontier.RunPending(seeded.RunID); pending != pagesPerRun {
			t.Fatalf("run %d pending = %d, want %d", run, pending, pagesPerRun)
		}
	}
	if len(crawlFrontier.state.ready) > crawlFrontier.maxReady {
		t.Fatalf(
			"global ready pages = %d, max %d",
			len(crawlFrontier.state.ready),
			crawlFrontier.maxReady,
		)
	}
}

func TestBoundedRecoveryKeepsPersistedDedupAndExtendsCursorForNewAdmissions(t *testing.T) {
	checkpoint := openBoundedRecoveryCheckpoint(t)
	profile := internalProfile(t)
	provenance := []byte("bounded-dedup-run")
	identity := []byte("bounded-dedup-order")
	persistBoundedRecoveryPages(t, boundedRecoveryPersistence{
		checkpoint: checkpoint, provenance: provenance, identity: identity,
		profileHandle: profile.Profile.Handle, total: 300,
	})
	crawlFrontier := NewFrontier(16, nil, WithCheckpoint(checkpoint))
	finished := make(chan bool, 1)
	seeded := crawlFrontier.SeedRunWithPriority(
		context.Background(),
		CrawlRunSeed{Provenance: provenance, OrderIdentity: identity},
		profile,
		func(succeeded bool) { finished <- succeeded },
	)
	fixture := boundedDedupRecoveryFixture{
		checkpoint: checkpoint,
		frontier:   crawlFrontier,
		provenance: provenance,
		identity:   identity,
		runID:      seeded.RunID,
		held:       internalReceive(t, crawlFrontier),
		finished:   finished,
	}
	duplicateURL := boundedRecoveryPageURL(0, 299)
	newURL := "https://new-after-restart.example/page"
	upper := fixture.admitNewPage(t, duplicateURL, newURL)
	fixture.assertNewPageDurable(t, newURL, upper)
	fixture.cancel(t)
}

type boundedDedupRecoveryFixture struct {
	checkpoint *frontiercheckpoint.FrontierCheckpoint
	frontier   *Frontier
	provenance []byte
	identity   []byte
	runID      uuid.UUID
	held       crawljob.CrawlJob
	finished   chan bool
}

func (fixture boundedDedupRecoveryFixture) admitNewPage(
	t *testing.T,
	duplicateURL string,
	newURL string,
) uint64 {
	t.Helper()
	fixture.frontier.mu.Lock()
	previousUpper := fixture.frontier.state.runs[fixture.runID].recoveryUpper
	fixture.frontier.mu.Unlock()
	duplicates := fixture.frontier.Submit(
		context.Background(),
		fixture.held,
		crawljob.DiscoveredLinks{Followable: []string{duplicateURL, newURL}},
	)
	if duplicates != 1 {
		t.Fatalf("persisted duplicate total = %d, want 1", duplicates)
	}
	fixture.frontier.mu.Lock()
	recovered := fixture.frontier.state.runs[fixture.runID]
	upper := recovered.recoveryUpper
	_, duplicateVisited := recovered.visited[duplicateURL]
	_, newVisited := recovered.visited[newURL]
	complete := recovered.recoveryComplete
	fixture.frontier.mu.Unlock()
	if duplicateVisited {
		t.Fatal("pre-restart visited URL was materialized in memory")
	}
	if newVisited {
		t.Fatal("new admission was retained outside the durable frontier")
	}
	if upper != previousUpper+1 || complete {
		t.Fatalf(
			"extended recovery boundary = %d from %d, complete=%t",
			upper,
			previousUpper,
			complete,
		)
	}

	return upper
}

func (fixture boundedDedupRecoveryFixture) assertNewPageDurable(
	t *testing.T,
	newURL string,
	upper uint64,
) {
	t.Helper()
	state, err := fixture.checkpoint.Inspect(
		context.Background(),
		fixture.provenance,
		fixture.identity,
	)
	if err != nil || state.Pages != 301 || state.Pending != 301 {
		t.Fatalf("post-restart admission state = %+v, %v", state, err)
	}
	loaded, err := fixture.checkpoint.LoadRecoveryPageBatch(
		context.Background(),
		fixture.provenance,
		256,
		upper,
		frontiercheckpoint.RecoveryPageBatchSize,
	)
	if err != nil {
		t.Fatalf("inspect remaining recovery boundary: %v", err)
	}
	foundNew := false
	for _, page := range loaded.Pages {
		if page.URL == newURL {
			foundNew = true
		}
	}
	if !foundNew {
		t.Fatal("new admission was not reachable through the extended recovery cursor")
	}
}

func (fixture boundedDedupRecoveryFixture) cancel(t *testing.T) {
	t.Helper()
	if !fixture.frontier.CancelControl(fixture.provenance) {
		t.Fatal("cancel bounded recovered run")
	}
	if pending := fixture.frontier.RunPending(fixture.runID); pending != 1 {
		t.Fatalf("pending after bounded cancellation = %d, want held page", pending)
	}
	fixture.frontier.Done(fixture.held, successfulPageOutcome())
	select {
	case succeeded := <-fixture.finished:
		if !succeeded {
			t.Fatal("cancelled bounded run reported checkpoint failure")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled bounded run did not settle")
	}
	state, err := fixture.checkpoint.Inspect(
		context.Background(),
		fixture.provenance,
		fixture.identity,
	)
	if err != nil || state.Status != frontiercheckpoint.RunCompleted || state.Pending != 0 {
		t.Fatalf("cancelled bounded checkpoint = %+v, %v", state, err)
	}
}

func TestBoundedRecoveryRefillKeepsOneLiveWindow(t *testing.T) {
	checkpoint := openBoundedRecoveryCheckpoint(t)
	profile := internalProfile(t)
	provenance := []byte("bounded-refill-window")
	identity := []byte("bounded-refill-window-order")
	persistBoundedRecoveryPages(t, boundedRecoveryPersistence{
		checkpoint: checkpoint, provenance: provenance, identity: identity,
		profileHandle: profile.Profile.Handle, total: 1_025,
	})
	crawlFrontier := NewFrontier(32, nil, WithCheckpoint(checkpoint))
	seeded := crawlFrontier.SeedRunWithPriority(
		context.Background(),
		CrawlRunSeed{Provenance: provenance, OrderIdentity: identity},
		profile,
		func(bool) { t.Error("bounded refill run unexpectedly settled") },
	)
	crawlFrontier.mu.Lock()
	initialCursor := crawlFrontier.state.runs[seeded.RunID].recoveryCursor
	crawlFrontier.mu.Unlock()
	for range 130 {
		job := internalReceive(t, crawlFrontier)
		crawlFrontier.Done(job, successfulPageOutcome())
	}
	crawlFrontier.mu.Lock()
	run := crawlFrontier.state.runs[seeded.RunID]
	live := run.pendingPages + crawlFrontier.readyPerRun[seeded.RunID]
	advanced := run.recoveryCursor
	crawlFrontier.mu.Unlock()
	if live > frontiercheckpoint.RecoveryPageBatchSize || advanced <= initialCursor {
		t.Fatalf("bounded refill cursor=%d, initial=%d live=%d", advanced, initialCursor, live)
	}
}

func TestInitiallyRetiredBoundedRecoverySettlesWithoutDispatch(t *testing.T) {
	checkpoint := openBoundedRecoveryCheckpoint(t)
	profile := internalProfile(t)
	provenance := []byte("bounded-retired-frontier")
	identity := []byte("bounded-retired-frontier-order")
	const pages = frontiercheckpoint.RecoveryPageBatchSize + 37
	persistBoundedRecoveryPages(t, boundedRecoveryPersistence{
		checkpoint: checkpoint, provenance: provenance, identity: identity,
		profileHandle: profile.Profile.Handle, total: pages,
	})
	if err := checkpoint.RecordHostState(
		context.Background(),
		provenance,
		boundedRecoveryHost(0, 0),
		frontiercheckpoint.HostProgress{Generation: 1, Retired: true},
		nil,
	); err != nil {
		t.Fatalf("retire recovered host: %v", err)
	}

	crawlFrontier := NewFrontier(8, nil, WithCheckpoint(checkpoint))
	finished := make(chan bool, 1)
	seeded := crawlFrontier.SeedRunWithPriority(
		context.Background(),
		CrawlRunSeed{Provenance: provenance, OrderIdentity: identity},
		profile,
		func(succeeded bool) { finished <- succeeded },
	)
	if seeded.Queued != 37 || crawlFrontier.RunPending(seeded.RunID) != 37 {
		t.Fatalf(
			"retired recovery queued=%d pending=%d",
			seeded.Queued,
			crawlFrontier.RunPending(seeded.RunID),
		)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if job, ok := crawlFrontier.Take(ctx); ok {
		t.Fatalf("retired recovery dispatched %q", job.URL)
	}
	select {
	case succeeded := <-finished:
		if !succeeded {
			t.Fatal("retired bounded recovery reported failure")
		}
	case <-time.After(time.Second):
		t.Fatal("retired bounded recovery did not settle")
	}
}

func TestLargeInterruptedSeedManifestRestoresThroughBoundedProducer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bounded-seed-restart.db")
	checkpoint, err := frontiercheckpoint.Open(path)
	if err != nil {
		t.Fatalf("open interrupted seed checkpoint: %v", err)
	}
	profile := internalProfile(t)
	provenance := []byte("bounded-seed-restart")
	identity := []byte("bounded-seed-restart-order")
	const total = 50_000
	pages := largeInterruptedSeedPages(profile.Profile.Handle, total)
	persistInterruptedSeedManifest(t, checkpoint, provenance, identity, pages)
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close interrupted seed checkpoint: %v", err)
	}
	checkpoint, err = frontiercheckpoint.Open(path)
	if err != nil {
		t.Fatalf("reopen interrupted seed checkpoint: %v", err)
	}
	t.Cleanup(func() { _ = checkpoint.Close() })

	crawlFrontier := NewFrontier(32, nil, WithCheckpoint(checkpoint))
	crawlFrontier.prepareSeeds = func(
		context.Context,
		[]yagocrawlcontract.CrawlRequest,
		[]byte,
		crawladmission.AdmissionProfile,
	) []frontierCandidate {
		t.Fatal("existing durable manifest was reparsed")

		return nil
	}
	seeded := crawlFrontier.SeedRunWithPriority(
		context.Background(),
		CrawlRunSeed{
			Requests:      internalRequests(profile, "https://must-not-be-reparsed.example/"),
			Provenance:    provenance,
			OrderIdentity: identity,
		},
		profile,
		func(bool) { t.Error("large interrupted seed unexpectedly settled") },
	)
	if seeded.Queued > frontiercheckpoint.RecoveryPageBatchSize {
		t.Fatalf(
			"initial interrupted seed queued = %d, max %d",
			seeded.Queued,
			frontiercheckpoint.RecoveryPageBatchSize,
		)
	}
	crawlFrontier.mu.Lock()
	run := crawlFrontier.state.runs[seeded.RunID]
	live := run.pendingPages + crawlFrontier.readyPerRun[seeded.RunID]
	cursor := run.seedRecoveryCursor
	length := run.seedRecoveryLength
	seedRecovery := run.seedRecovery
	crawlFrontier.mu.Unlock()
	if !seedRecovery || length != total || cursor > frontiercheckpoint.RecoveryPageBatchSize ||
		live > frontiercheckpoint.RecoveryPageBatchSize {
		t.Fatalf(
			"interrupted seed recovery=%t cursor=%d/%d live=%d",
			seedRecovery,
			cursor,
			length,
			live,
		)
	}
	for range 130 {
		job := internalReceive(t, crawlFrontier)
		crawlFrontier.Done(job, successfulPageOutcome())
	}
	crawlFrontier.mu.Lock()
	run = crawlFrontier.state.runs[seeded.RunID]
	live = run.pendingPages + crawlFrontier.readyPerRun[seeded.RunID]
	advanced := run.seedRecoveryCursor
	crawlFrontier.mu.Unlock()
	if advanced <= cursor || advanced >= total || live > frontiercheckpoint.RecoveryPageBatchSize {
		t.Fatalf("advanced interrupted seed cursor=%d, initial=%d live=%d", advanced, cursor, live)
	}
}

func TestBoundedSeedCancellationRemovesTheManifestWithoutStrandingCompletion(t *testing.T) {
	checkpoint := openBoundedRecoveryCheckpoint(t)
	profile := internalProfile(t)
	provenance := []byte("bounded-seed-cancellation")
	identity := []byte("bounded-seed-cancellation-order")
	const total = frontiercheckpoint.RecoveryPageBatchSize*2 + 1
	urls := make([]string, 0, total)
	for page := range total {
		urls = append(urls, fmt.Sprintf("https://seed-cancel.example/page/%05d", page))
	}
	crawlFrontier := NewFrontier(32, nil, WithCheckpoint(checkpoint))
	finished := make(chan bool, 1)
	seeded := crawlFrontier.SeedRunWithPriority(
		context.Background(),
		CrawlRunSeed{
			Requests:      internalRequests(profile, urls...),
			Provenance:    provenance,
			OrderIdentity: identity,
		},
		profile,
		func(succeeded bool) { finished <- succeeded },
	)
	if seeded.Queued > frontiercheckpoint.RecoveryPageBatchSize {
		t.Fatalf(
			"initial bounded seed queued = %d, max %d",
			seeded.Queued,
			frontiercheckpoint.RecoveryPageBatchSize,
		)
	}
	if !crawlFrontier.CancelControl(provenance) {
		t.Fatal("cancel bounded seed run")
	}
	if pending := crawlFrontier.RunPending(seeded.RunID); pending != 0 {
		t.Fatalf("bounded seed pending after cancellation = %d, want 0", pending)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if job, ok := crawlFrontier.Take(ctx); ok {
		t.Fatalf("cancelled bounded seed dispatched %q", job.URL)
	}
	select {
	case succeeded := <-finished:
		if !succeeded {
			t.Fatal("cancelled bounded seed reported checkpoint failure")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled bounded seed did not settle")
	}
	state, err := checkpoint.Inspect(context.Background(), provenance, identity)
	if err != nil || state.Status != frontiercheckpoint.RunCompleted ||
		state.Pending != 0 || state.Seeding || state.SeedManifest {
		t.Fatalf("cancelled bounded seed checkpoint = %+v, %v", state, err)
	}
}

func openBoundedRecoveryCheckpoint(t *testing.T) *frontiercheckpoint.FrontierCheckpoint {
	t.Helper()
	checkpoint, err := frontiercheckpoint.Open(filepath.Join(t.TempDir(), "bounded-frontier.db"))
	if err != nil {
		t.Fatalf("open bounded recovery checkpoint: %v", err)
	}
	t.Cleanup(func() {
		if err := checkpoint.Close(); err != nil {
			t.Errorf("close bounded recovery checkpoint: %v", err)
		}
	})

	return checkpoint
}

func largeInterruptedSeedPages(profileHandle string, total int) []frontiercheckpoint.Page {
	pages := make([]frontiercheckpoint.Page, 0, total)
	for page := range total {
		pages = append(pages, frontiercheckpoint.Page{
			URL:           fmt.Sprintf("https://seed-restart.example/page/%05d", page),
			Host:          "seed-restart.example",
			ProfileHandle: profileHandle,
			ObservationID: fmt.Sprintf("seed-restart-%05d", page),
			ObservedAt:    time.Date(2026, 7, 17, 10, 0, page, 0, time.UTC),
			Index:         true,
		})
	}

	return pages
}

func persistInterruptedSeedManifest(
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
		t.Fatalf("publish large seed manifest: %v", err)
	}
	first := pages[:17]
	decisions := make([]frontiercheckpoint.SeedDecision, 0, len(first))
	for _, page := range first {
		decisions = append(decisions, frontiercheckpoint.SeedDecision{Page: page, Admit: true})
	}
	result, err := checkpoint.AdmitSeedBatch(
		context.Background(),
		provenance,
		frontiercheckpoint.SeedBatch{Decisions: decisions},
	)
	if err != nil || result.Admitted != len(first) {
		t.Fatalf("admit interrupted seed prefix = %+v, %v", result, err)
	}
}

type boundedRecoveryPersistence struct {
	checkpoint    *frontiercheckpoint.FrontierCheckpoint
	provenance    []byte
	identity      []byte
	profileHandle string
	run           int
	total         int
}

func persistBoundedRecoveryPages(t *testing.T, persistence boundedRecoveryPersistence) {
	t.Helper()
	if err := persistence.checkpoint.Begin(
		context.Background(),
		persistence.provenance,
		persistence.identity,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("begin bounded run %d: %v", persistence.run, err)
	}
	for start := 0; start < persistence.total; start += frontierMutationBatchSize {
		end := min(start+frontierMutationBatchSize, persistence.total)
		pages := make([]frontiercheckpoint.Page, 0, end-start)
		for page := start; page < end; page++ {
			host := boundedRecoveryHost(persistence.run, page)
			pages = append(pages, frontiercheckpoint.Page{
				URL:           boundedRecoveryPageURL(persistence.run, page),
				Host:          host,
				Depth:         0,
				ProfileHandle: persistence.profileHandle,
				ObservationID: fmt.Sprintf("bounded-%02d-%05d", persistence.run, page),
				ObservedAt:    time.Date(2026, 7, 17, 9, 0, page, 0, time.UTC),
				Index:         true,
			})
		}
		admitted, err := persistence.checkpoint.Admit(
			context.Background(),
			persistence.provenance,
			pages,
		)
		if err != nil || admitted != len(pages) {
			t.Fatalf(
				"admit bounded run %d pages %d:%d = %d, %v",
				persistence.run,
				start,
				end,
				admitted,
				err,
			)
		}
	}
	if err := persistence.checkpoint.FinishSeeding(
		context.Background(),
		persistence.provenance,
		yagocrawlcontract.CrawlRunTally{},
	); err != nil {
		t.Fatalf("finish bounded run %d seeding: %v", persistence.run, err)
	}
}

func boundedRecoveryHost(run int, page int) string {
	if run == 0 {
		return "bounded-00.example"
	}

	return fmt.Sprintf("bounded-%02d-%05d.example", run, page)
}

func boundedRecoveryPageURL(run int, page int) string {
	return fmt.Sprintf("https://%s/page/%05d", boundedRecoveryHost(run, page), page)
}
