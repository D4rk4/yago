package frontier

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
)

func TestNewPendingHostEscapesAnAlreadyBlockedReadyHost(t *testing.T) {
	frontier := NewFrontier(1, nil, WithMaxHostConcurrency(1))
	profile := internalProfile(t)
	provenance := []byte("late-host")
	frontier.SeedRun(
		t.Context(),
		internalRequests(
			profile,
			"https://first.example/one",
			"https://first.example/two",
		),
		provenance,
		profile,
		nil,
	)
	first := internalReceive(t, frontier)
	frontier.Submit(t.Context(), first, crawljob.DiscoveredLinks{
		Followable: []string{"https://second.example/one"},
	})
	second := internalReceive(t, frontier)
	if second.URL != "https://second.example/one" {
		t.Fatalf("second job = %q, want late host", second.URL)
	}
	frontier.Done(first, false)
	frontier.Done(second, false)
	frontier.Cancel(provenance)
}

func TestEligibilityRebuildClaimsAHostBeyondBlockedReadyRuns(t *testing.T) {
	frontier := NewFrontier(2, nil, WithMaxHostConcurrency(1))
	profile := internalProfile(t)
	firstRun := frontier.SeedRun(
		t.Context(),
		internalRequests(
			profile,
			"https://shared.example/first",
			"https://shared.example/second",
		),
		[]byte("first-shared-run"),
		profile,
		nil,
	)
	frontier.SeedRun(
		t.Context(),
		internalRequests(
			profile,
			"https://shared.example/third",
			"https://other.example/first",
		),
		[]byte("second-shared-run"),
		profile,
		nil,
	)
	frontier.mu.Lock()
	frontier.dispatchOrder[firstRun.RunID] = 0
	for runID := range frontier.state.runs {
		if runID != firstRun.RunID {
			frontier.dispatchOrder[runID] = 1
		}
	}
	frontier.mu.Unlock()
	first := internalReceive(t, frontier)
	if first.RunID != firstRun.RunID {
		t.Fatalf("first claimed run = %s, want %s", first.RunID, firstRun.RunID)
	}
	second := internalReceive(t, frontier)
	if second.URL != "https://other.example/first" {
		t.Fatalf("rebuilt job = %q, want other host", second.URL)
	}
	frontier.Done(first, false)
	frontier.Done(second, false)
	frontier.Cancel([]byte("first-shared-run"))
	frontier.Cancel([]byte("second-shared-run"))
}

func TestPauseDemotesAnAlreadyReadyRun(t *testing.T) {
	frontier := NewFrontier(1, nil)
	profile := internalProfile(t)
	provenance := []byte("ready-pause")
	frontier.SeedRun(
		t.Context(),
		internalRequests(profile, "https://example.org/"),
		provenance,
		profile,
		nil,
	)
	frontier.Pause(provenance)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if job, ok := frontier.Take(ctx); ok {
		t.Fatalf("paused ready run returned %q", job.URL)
	}
	frontier.Resume(provenance)
	job := internalReceive(t, frontier)
	frontier.Done(job, false)
}

func TestExpiredPendingControlIsPurged(t *testing.T) {
	frontier := NewFrontier(1, nil)
	expired := "expired-control"
	live := "live-control"
	now := time.Now()
	frontier.controlSeen[expired] = now.Add(-pendingControlTTL)
	frontier.controlSeen[live] = now
	frontier.paused[expired] = struct{}{}
	frontier.state.cancelled[expired] = struct{}{}
	frontier.rateInterval[expired] = time.Second
	frontier.rateNextDue[expired] = now
	frontier.purgePendingControlsLocked(now)
	if _, retained := frontier.controlSeen[expired]; retained {
		t.Fatal("expired control timestamp remains")
	}
	if _, retained := frontier.paused[expired]; retained {
		t.Fatal("expired pause remains")
	}
	if _, retained := frontier.state.cancelled[expired]; retained {
		t.Fatal("expired cancellation remains")
	}
	if _, retained := frontier.rateInterval[expired]; retained {
		t.Fatal("expired rate remains")
	}
	if _, retained := frontier.controlSeen[live]; !retained {
		t.Fatal("live control was purged")
	}
}

func TestFutureRateSelectionIgnoresInactiveRuns(t *testing.T) {
	frontier := NewFrontier(1, nil)
	profile := internalProfile(t)
	now := time.Now()
	activeID := uuid.New()
	frontier.state.beginRun(activeID, []byte("active-rate"), profile, nil)
	active := frontier.state.runs[activeID]
	active.seeding = false
	active.appendPending(
		frontierCandidate{normURL: "https://active.example/", host: "active.example"},
	)
	frontier.rateInterval[active.provenance] = time.Second
	frontier.rateNextDue[active.provenance] = now.Add(time.Second)
	pausedID := uuid.New()
	frontier.state.beginRun(pausedID, []byte("paused-rate"), profile, nil)
	paused := frontier.state.runs[pausedID]
	paused.seeding = false
	paused.appendPending(
		frontierCandidate{normURL: "https://paused.example/", host: "paused.example"},
	)
	frontier.paused[paused.provenance] = struct{}{}
	frontier.rateInterval[paused.provenance] = time.Second
	frontier.rateNextDue[paused.provenance] = now.Add(time.Millisecond)
	seedingID := uuid.New()
	frontier.state.beginRun(seedingID, []byte("seeding-rate"), profile, nil)
	seeding := frontier.state.runs[seedingID]
	seeding.appendPending(
		frontierCandidate{normURL: "https://seeding.example/", host: "seeding.example"},
	)
	frontier.rateInterval[seeding.provenance] = time.Second
	frontier.rateNextDue[seeding.provenance] = now.Add(time.Millisecond)
	emptyID := uuid.New()
	frontier.state.beginRun(emptyID, []byte("empty-rate"), profile, nil)
	empty := frontier.state.runs[emptyID]
	empty.seeding = false
	frontier.rateInterval[empty.provenance] = time.Second
	frontier.rateNextDue[empty.provenance] = now.Add(time.Millisecond)
	excluded, wait := frontier.futureRateRunsLocked(now)
	if len(excluded) != 1 {
		t.Fatalf("excluded future-rate runs = %d, want 1", len(excluded))
	}
	if _, found := excluded[activeID]; !found {
		t.Fatal("active future-rate run was not excluded")
	}
	if wait <= 0 || wait > time.Second {
		t.Fatalf("future-rate wait = %s", wait)
	}
}

func TestEarlierWaitKeepsTheNearestPositiveDelay(t *testing.T) {
	if got := earlierWait(0, 0); got != 0 {
		t.Fatalf("zero waits = %s", got)
	}
	if got := earlierWait(time.Second, 0); got != time.Second {
		t.Fatalf("ignored wait = %s", got)
	}
	if got := earlierWait(0, time.Second); got != time.Second {
		t.Fatalf("first positive wait = %s", got)
	}
	if got := earlierWait(time.Second, 2*time.Second); got != time.Second {
		t.Fatalf("farther wait = %s", got)
	}
	if got := earlierWait(2*time.Second, time.Second); got != time.Second {
		t.Fatalf("nearer wait = %s", got)
	}
}

func TestWaitForSettlementsBlocksUntilCallbackReturns(t *testing.T) {
	frontier := NewFrontier(1, nil)
	started := make(chan struct{})
	release := make(chan struct{})
	frontier.scheduleSettlement(func(bool) {
		close(started)
		<-release
	}, true)
	<-started
	done := make(chan struct{})
	go func() {
		frontier.WaitForSettlements()
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("settlement wait returned before callback")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("settlement wait did not return")
	}
}

func TestCancelBeforeRunRegistrationMarksSeededRunCancelled(t *testing.T) {
	frontier := NewFrontier(1, nil)
	profile := internalProfile(t)
	provenance := []byte("pre-cancelled")
	frontier.Cancel(provenance)
	finished := make(chan bool, 1)
	seeded := frontier.SeedRun(
		t.Context(),
		internalRequests(profile, "https://example.org/"),
		provenance,
		profile,
		func(succeeded bool) { finished <- succeeded },
	)
	if seeded.Queued != 0 {
		t.Fatalf("pre-cancelled queued = %d, want 0", seeded.Queued)
	}
	select {
	case succeeded := <-finished:
		if !succeeded {
			t.Fatal("empty pre-cancelled run did not settle cleanly")
		}
	case <-time.After(time.Second):
		t.Fatal("pre-cancelled run did not settle")
	}
	if !frontier.WasCancelled(provenance) {
		t.Fatal("pre-cancelled provenance was not retained")
	}
	frontier.ClearCancelled(provenance)
}

func TestDefaultFrontierNoopBoundaries(t *testing.T) {
	now := time.Now()
	pace := alwaysDuePace{}
	if due := pace.DueAt(crawljob.CrawlJob{}, now); !due.Equal(now) {
		t.Fatalf("default due = %s, want %s", due, now)
	}
	pace.Visited(crawljob.CrawlJob{}, now)
	noopRunTally{}.Duplicate(nil)
}
