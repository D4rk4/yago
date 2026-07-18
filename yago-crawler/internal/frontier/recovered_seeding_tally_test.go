package frontier_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yago-crawler/internal/runtally"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type finishSeedingFailureCheckpoint struct {
	frontier.Checkpoint
	err error
}

func (checkpoint finishSeedingFailureCheckpoint) FinishSeeding(
	context.Context,
	[]byte,
	yagocrawlcontract.CrawlRunTally,
) error {
	return checkpoint.err
}

func TestRecoveredSeedingDoesNotRecountPersistedPrefixAcrossRestarts(t *testing.T) {
	scenario := newRecoveredSeedTallyScenario(t)
	persistRecoveredSeedTallyPrefix(t, scenario)
	interruptRecoveredSeedTallySeeding(t, scenario)
	assertRecoveredSeedTallyCompletion(t, scenario)
}

type recoveredSeedTallyScenario struct {
	path       string
	profile    crawladmission.AdmissionProfile
	provenance []byte
	identity   []byte
}

func newRecoveredSeedTallyScenario(t *testing.T) recoveredSeedTallyScenario {
	t.Helper()
	return recoveredSeedTallyScenario{
		path: filepath.Join(t.TempDir(), "frontier-v1.db"),
		profile: compiled(t, yagocrawlcontract.CrawlProfile{
			Scope:           yagocrawlcontract.ScopeDomain,
			URLMustMatch:    yagocrawlcontract.MatchAll,
			MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
		}),
		provenance: []byte("recovered-seed-tally"),
		identity:   []byte("recovered-seed-tally-order"),
	}
}

func recoveredSeedTallyPages(profileHandle string) []frontiercheckpoint.Page {
	firstPage := frontiercheckpoint.Page{
		URL:           "https://example.com/first",
		Host:          "example.com",
		ProfileHandle: profileHandle,
		ObservationID: "first-seed-observation",
		ObservedAt:    time.Date(2026, 7, 17, 3, 0, 0, 0, time.UTC),
		Index:         true,
	}
	duplicatePage := firstPage
	duplicatePage.ObservationID = "duplicate-seed-observation"
	duplicatePage.ObservedAt = duplicatePage.ObservedAt.Add(time.Minute)
	secondPage := firstPage
	secondPage.URL = "https://example.com/second"
	secondPage.ObservationID = "second-seed-observation"
	secondPage.ObservedAt = secondPage.ObservedAt.Add(2 * time.Minute)

	return []frontiercheckpoint.Page{firstPage, duplicatePage, secondPage}
}

func persistRecoveredSeedTallyPrefix(t *testing.T, scenario recoveredSeedTallyScenario) {
	t.Helper()
	checkpoint, err := frontiercheckpoint.Open(scenario.path)
	if err != nil {
		t.Fatalf("open initial checkpoint: %v", err)
	}
	pages := recoveredSeedTallyPages(scenario.profile.Profile.Handle)
	if err := checkpoint.BeginSeedManifest(
		context.Background(),
		scenario.provenance,
		scenario.identity,
		yagocrawlcontract.CrawlOrderPriorityNormal,
		pages,
	); err != nil {
		t.Fatalf("begin initial checkpoint: %v", err)
	}
	result, err := checkpoint.AdmitSeedBatch(
		context.Background(),
		scenario.provenance,
		frontiercheckpoint.SeedBatch{
			Decisions: []frontiercheckpoint.SeedDecision{{Page: pages[0], Admit: true}},
		},
	)
	if err != nil || result.Admitted != 1 {
		t.Fatalf("admit initial seed = %+v, %v", result, err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close initial checkpoint: %v", err)
	}
}

func interruptRecoveredSeedTallySeeding(t *testing.T, scenario recoveredSeedTallyScenario) {
	t.Helper()
	checkpoint, err := frontiercheckpoint.Open(scenario.path)
	if err != nil {
		t.Fatalf("reopen interrupted checkpoint: %v", err)
	}
	finishFailure := errors.New("finish seeding interrupted")
	crawlFrontier := frontier.NewFrontier(
		2,
		nil,
		frontier.WithCheckpoint(finishSeedingFailureCheckpoint{
			Checkpoint: checkpoint,
			err:        finishFailure,
		}),
	)
	seeded := crawlFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{
			Provenance:    scenario.provenance,
			OrderIdentity: scenario.identity,
		},
		scenario.profile,
		func(bool) { t.Error("interrupted recovery unexpectedly settled") },
	)
	if seeded.Queued != 2 || !errors.Is(crawlFrontier.CheckpointFailure(), finishFailure) {
		t.Fatalf("interrupted seed = %+v, failure = %v", seeded, crawlFrontier.CheckpointFailure())
	}
	state, err := checkpoint.Inspect(
		context.Background(),
		scenario.provenance,
		scenario.identity,
	)
	if err != nil || state.Tally.Duplicates != 1 || !state.Seeding ||
		state.Pages != 2 || state.Pending != 2 {
		t.Fatalf("interrupted checkpoint = %+v, %v", state, err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close interrupted checkpoint: %v", err)
	}
}

func assertRecoveredSeedTallyCompletion(t *testing.T, scenario recoveredSeedTallyScenario) {
	t.Helper()
	checkpoint, err := frontiercheckpoint.Open(scenario.path)
	if err != nil {
		t.Fatalf("reopen recovered checkpoint: %v", err)
	}
	t.Cleanup(func() { _ = checkpoint.Close() })
	tally := runtally.New()
	crawlFrontier := frontier.NewFrontier(
		2,
		nil,
		frontier.WithCheckpoint(checkpoint),
		frontier.WithRunTally(tally),
	)
	finished := make(chan bool, 1)
	seeded := crawlFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{
			Provenance:    scenario.provenance,
			OrderIdentity: scenario.identity,
		},
		scenario.profile,
		func(succeeded bool) { finished <- succeeded },
	)
	if seeded.Queued != 2 {
		t.Fatalf("recovered queued = %d, want 2", seeded.Queued)
	}
	if got := tally.Snapshot(scenario.provenance).Duplicates; got != 1 {
		t.Fatalf("recovered duplicate tally = %d, want only source duplicate", got)
	}
	for range 2 {
		crawlFrontier.Done(
			receiveJob(t, crawlFrontier),
			yagocrawlcontract.CrawlRunTally{Fetched: 1},
		)
	}
	expectSuccessfulSettlement(t, finished, "recovered run")
	state, err := checkpoint.Inspect(
		context.Background(),
		scenario.provenance,
		scenario.identity,
	)
	if err != nil || state.Tally.Duplicates != 1 || state.Tally.Fetched != 2 ||
		state.Status != frontiercheckpoint.RunCompleted {
		t.Fatalf("final checkpoint = %+v, %v", state, err)
	}
}
