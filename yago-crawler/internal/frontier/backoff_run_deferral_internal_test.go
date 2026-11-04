package frontier

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlrun"
)

type backoffDeadlineTestPace struct {
	deadlines map[string]time.Time
}

func (pace backoffDeadlineTestPace) DueAt(_ crawljob.CrawlJob, now time.Time) time.Time {
	return now
}

func (backoffDeadlineTestPace) Visited(crawljob.CrawlJob, time.Time) {}

func (pace backoffDeadlineTestPace) BackoffUntil(rawURL string) time.Time {
	return pace.deadlines[rawURL]
}

func TestBackoffOnlyRunRequiresEveryQueuedPageToRemainBackedOff(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	readyURL := "https://ready.example/page"
	pendingURL := "https://pending.example/page"
	readyDeadline := now.Add(2 * time.Minute)
	deadlinePace := backoffDeadlineTestPace{deadlines: map[string]time.Time{
		readyURL:   readyDeadline,
		pendingURL: now.Add(4 * time.Minute),
	}}
	frontier, runID, run := newBackoffRunFixture(deadlinePace)

	frontier.mu.Lock()
	deadline, eligible := frontier.backoffOnlyRunLocked(
		runID,
		run,
		now,
		deadlinePace,
	)
	frontier.mu.Unlock()
	if !eligible || !deadline.Equal(readyDeadline) {
		t.Fatalf("backed-off run = %v/%v, want %v/true", deadline, eligible, readyDeadline)
	}
}

func TestBackoffOnlyRunRejectsIneligibleRunStates(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	deadlinePace := backoffDeadlineTestPace{deadlines: map[string]time.Time{
		"https://ready.example/page":   now.Add(time.Minute),
		"https://pending.example/page": now.Add(2 * time.Minute),
	}}
	mutations := []struct {
		name   string
		change func(*Frontier, *crawlRun)
	}{
		{name: "seeding", change: func(_ *Frontier, run *crawlRun) { run.seeding = true }},
		{
			name:   "durability",
			change: func(_ *Frontier, run *crawlRun) { run.awaitingDurability = true },
		},
		{name: "cancelled", change: func(_ *Frontier, run *crawlRun) { run.cancelled = true }},
		{
			name:   "seed recovery",
			change: func(_ *Frontier, run *crawlRun) { run.seedRecovery = true },
		},
		{
			name:   "seed finishing",
			change: func(_ *Frontier, run *crawlRun) { run.seedFinishing = true },
		},
		{
			name: "incomplete bounded recovery",
			change: func(_ *Frontier, run *crawlRun) {
				run.boundedRecovery = true
				run.recoveryComplete = false
			},
		},
		{
			name: "paused",
			change: func(frontier *Frontier, run *crawlRun) {
				frontier.paused[run.provenance] = struct{}{}
			},
		},
		{
			name: "suspended",
			change: func(frontier *Frontier, run *crawlRun) {
				frontier.suspended[run.provenance] = struct{}{}
			},
		},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			frontier, runID, run := newBackoffRunFixture(deadlinePace)
			mutation.change(frontier, run)
			frontier.mu.Lock()
			_, eligible := frontier.backoffOnlyRunLocked(runID, run, now, deadlinePace)
			frontier.mu.Unlock()
			if eligible {
				t.Fatal("ineligible run was marked backed off")
			}
		})
	}
}

func TestBackoffOnlyRunRejectsIncompletePageAccounting(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	deadlinePace := backoffDeadlineTestPace{deadlines: map[string]time.Time{
		"https://ready.example/page":   now.Add(time.Minute),
		"https://pending.example/page": now.Add(2 * time.Minute),
	}}
	frontier, runID, run := newBackoffRunFixture(deadlinePace)
	frontier.state.completion = crawlrun.NewCompletion()

	frontier.mu.Lock()
	_, eligible := frontier.backoffOnlyRunLocked(runID, run, now, deadlinePace)
	frontier.mu.Unlock()
	if eligible {
		t.Fatal("run with unmatched completion accounting was deferred")
	}
}

func TestBackoffOnlyRunRejectsReadyPageWithoutFutureHostDeadline(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	deadlinePace := backoffDeadlineTestPace{deadlines: map[string]time.Time{
		"https://ready.example/page": now,
	}}
	frontier, runID, run := newBackoffRunFixture(deadlinePace)
	run.pendingPages = 0
	run.pendingHosts = nil
	run.pendingByHost = make(map[string]*pendingHostPages)
	run.pendingHostLive = 0
	frontier.state.completion = crawlrun.NewCompletion()
	frontier.state.completion.Begin(runID, nil)

	frontier.mu.Lock()
	_, eligible := frontier.backoffOnlyRunLocked(runID, run, now, deadlinePace)
	frontier.mu.Unlock()
	if eligible {
		t.Fatal("ready page without a future host deadline was deferred")
	}
}

func TestBackoffOnlyRunIgnoresReadyPagesFromOtherRuns(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	deadlinePace := backoffDeadlineTestPace{deadlines: map[string]time.Time{
		"https://ready.example/page":   now.Add(time.Minute),
		"https://pending.example/page": now.Add(2 * time.Minute),
	}}
	frontier, runID, run := newBackoffRunFixture(deadlinePace)
	frontier.state.ready = append(frontier.state.ready, crawljob.CrawlJob{
		RunID: uuid.New(),
		URL:   "https://unrelated.example/page",
	})

	frontier.mu.Lock()
	_, eligible := frontier.backoffOnlyRunLocked(runID, run, now, deadlinePace)
	frontier.mu.Unlock()
	if !eligible {
		t.Fatal("an unrelated ready page prevented an otherwise backed-off run from deferring")
	}
}

func TestBackoffOnlyRunRejectsPendingPageWithoutFutureHostDeadline(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	deadlinePace := backoffDeadlineTestPace{deadlines: map[string]time.Time{
		"https://ready.example/page":   now.Add(time.Minute),
		"https://pending.example/page": now,
	}}
	frontier, runID, run := newBackoffRunFixture(deadlinePace)

	frontier.mu.Lock()
	_, eligible := frontier.backoffOnlyRunLocked(runID, run, now, deadlinePace)
	frontier.mu.Unlock()
	if eligible {
		t.Fatal("pending page without a future host deadline was deferred")
	}
}

func TestBackoffOnlyRunRejectsMissingAndEmptyPendingHostBuckets(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	deadlinePace := backoffDeadlineTestPace{deadlines: map[string]time.Time{}}
	for _, test := range []struct {
		name   string
		bucket *pendingHostPages
	}{
		{name: "missing"},
		{name: "empty", bucket: &pendingHostPages{host: "pending.example", slot: 0}},
	} {
		t.Run(test.name, func(t *testing.T) {
			frontier, runID, run := newBackoffRunFixture(deadlinePace)
			run.pendingHosts = []*pendingHostPages{test.bucket}
			run.pendingHostLive = 1
			run.pendingPages = 1
			frontier.readyPerRun[runID] = 0
			frontier.state.ready = nil
			frontier.state.completion = crawlrun.NewCompletion()
			frontier.state.completion.Begin(runID, nil)
			frontier.mu.Lock()
			_, eligible := frontier.backoffOnlyRunLocked(runID, run, now, deadlinePace)
			frontier.mu.Unlock()
			if eligible {
				t.Fatal("unrepresented pending page was deferred")
			}
		})
	}
}

func TestDeferBackedOffRunsWithoutBackoffPaceReturnsNoRuns(t *testing.T) {
	frontier := NewFrontier(1, nil)
	if deferred := frontier.DeferBackedOffRuns(time.Now()); deferred != nil {
		t.Fatalf("deferred runs without a backoff pace = %#v, want nil", deferred)
	}
}

func newBackoffRunFixture(
	pace CrawlPace,
) (*Frontier, uuid.UUID, *crawlRun) {
	frontier := NewFrontier(2, pace)
	runID := uuid.New()
	run := &crawlRun{
		provenance:      "backoff-run",
		provenanceValue: []byte("backoff-run"),
		pendingPages:    1,
		pendingByHost:   make(map[string]*pendingHostPages),
	}
	bucket := &pendingHostPages{
		host: "pending.example",
		slot: 0,
		queued: []pendingPage{{
			normURL: "https://pending.example/page",
		}},
	}
	run.pendingByHost[bucket.host] = bucket
	run.pendingHosts = []*pendingHostPages{bucket}
	run.pendingHostLive = 1
	frontier.state.runs[runID] = run
	frontier.readyPerRun[runID] = 1
	frontier.state.ready = []crawljob.CrawlJob{{
		RunID: runID,
		URL:   "https://ready.example/page",
	}}
	frontier.state.completion.Begin(runID, nil)
	frontier.state.completion.Track(runID)

	return frontier, runID, run
}
