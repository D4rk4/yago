package frontier

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
)

type countedDuePace struct {
	calls atomic.Int64
}

func (p *countedDuePace) DueAt(_ crawljob.CrawlJob, now time.Time) time.Time {
	p.calls.Add(1)

	return now
}

func (*countedDuePace) Visited(crawljob.CrawlJob, time.Time) {}

type blockingFuturePace struct {
	calls   atomic.Int64
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *blockingFuturePace) DueAt(_ crawljob.CrawlJob, now time.Time) time.Time {
	p.calls.Add(1)
	p.once.Do(func() {
		close(p.entered)
		<-p.release
	})

	return now.Add(time.Hour)
}

func (*blockingFuturePace) Visited(crawljob.CrawlJob, time.Time) {}

func TestReadyWindowPreservesAllWorkAndStaysFairAcrossTwentyRuns(t *testing.T) {
	const (
		capacity = 64
		runs     = 20
		perRun   = 100
	)
	frontier := NewFrontier(capacity, nil)
	finished := seedReadyCapacityRuns(t, frontier, runs, perRun)
	assertReadyCapacityDistribution(t, frontier, capacity, runs)
	seen := drainReadyCapacityRuns(t, frontier, capacity, runs*perRun)
	for range runs {
		<-finished
	}
	if len(seen) != runs*perRun {
		t.Fatalf("unique jobs = %d, want %d", len(seen), runs*perRun)
	}
	assertReadyCapacityReleased(t, frontier)
}

func seedReadyCapacityRuns(
	t *testing.T,
	frontier *Frontier,
	runs int,
	perRun int,
) <-chan struct{} {
	t.Helper()
	profile := internalProfile(t)
	finished := make(chan struct{}, runs)
	for run := range runs {
		provenance := []byte(fmt.Sprintf("run-%02d", run))
		requests := make([]string, 0, perRun)
		for page := range perRun {
			requests = append(
				requests,
				fmt.Sprintf("https://run-%02d.example/page/%03d", run, page),
			)
		}
		seeded := frontier.SeedRun(
			context.Background(),
			internalRequests(profile, requests...),
			provenance,
			profile,
			func(bool) { finished <- struct{}{} },
		)
		if seeded.Queued != perRun {
			t.Fatalf("run %d queued = %d, want %d", run, seeded.Queued, perRun)
		}
	}

	return finished
}

func assertReadyCapacityDistribution(
	t *testing.T,
	frontier *Frontier,
	capacity int,
	runs int,
) {
	t.Helper()
	frontier.mu.Lock()
	defer frontier.mu.Unlock()
	if len(frontier.state.ready) != capacity {
		t.Fatalf("ready jobs = %d, want %d", len(frontier.state.ready), capacity)
	}
	minimum, maximum := capacity, 0
	for _, ready := range frontier.readyPerRun {
		minimum = min(minimum, ready)
		maximum = max(maximum, ready)
	}
	if len(frontier.readyPerRun) != runs || maximum-minimum > 1 {
		t.Fatalf("ready distribution = %v", frontier.readyPerRun)
	}
}

func drainReadyCapacityRuns(
	t *testing.T,
	frontier *Frontier,
	capacity int,
	total int,
) map[string]struct{} {
	t.Helper()
	seen := make(map[string]struct{}, total)
	for range total {
		job := internalReceive(t, frontier)
		if _, duplicate := seen[job.URL]; duplicate {
			t.Fatalf("duplicate job %q", job.URL)
		}
		seen[job.URL] = struct{}{}
		frontier.mu.Lock()
		if len(frontier.state.ready) > capacity {
			frontier.mu.Unlock()
			t.Fatalf("ready jobs = %d, exceed %d", len(frontier.state.ready), capacity)
		}
		frontier.mu.Unlock()
		frontier.Done(job, successfulPageOutcome())
	}

	return seen
}

func assertReadyCapacityReleased(t *testing.T, frontier *Frontier) {
	t.Helper()
	frontier.mu.Lock()
	defer frontier.mu.Unlock()
	if len(frontier.state.ready) != 0 || len(frontier.state.runs) != 0 {
		t.Fatalf(
			"retained frontier state: ready=%d runs=%d",
			len(frontier.state.ready),
			len(frontier.state.runs),
		)
	}
	retained := frontier.state.ready[:cap(frontier.state.ready)]
	for index, job := range retained {
		if job.URL != "" || job.ProfileHandle != "" || len(job.Provenance) != 0 {
			t.Fatalf("retained ready job %d = %#v", index, job)
		}
	}
}

func TestReadyAdmissionDoesNotInvokeDispatchScorer(t *testing.T) {
	const capacity = 256
	var calls atomic.Int64
	frontier := NewFrontier(
		capacity,
		nil,
		WithValueScorer(func(_ crawljob.CrawlJob, _ int) float64 {
			calls.Add(1)

			return 1
		}),
	)
	profile := internalProfile(t)
	requests := make([]string, 0, 50_000)
	for page := range 50_000 {
		requests = append(requests, fmt.Sprintf("https://example.org/page/%05d", page))
	}
	seeded := frontier.SeedRun(
		context.Background(),
		internalRequests(profile, requests...),
		[]byte("large-run"),
		profile,
		nil,
	)
	if seeded.Queued != len(requests) {
		t.Fatalf("queued = %d, want %d", seeded.Queued, len(requests))
	}
	if calls.Load() != 0 {
		t.Fatalf("scorer calls during admission = %d, want 0", calls.Load())
	}
	job := internalReceive(t, frontier)
	if calls.Load() > capacity {
		t.Fatalf("scorer calls on first take = %d, want at most %d", calls.Load(), capacity)
	}
	frontier.Cancel(job.Provenance)
	frontier.Done(job, successfulPageOutcome())
}

func TestDefaultRateMillionPageBacklogUsesBoundedEligibilityWork(t *testing.T) {
	const (
		capacity = 256
		runs     = 20
		perRun   = 50_000
	)
	pace := &countedDuePace{}
	frontier := NewFrontier(capacity, pace, WithDefaultRunRate(30))
	profile := internalProfile(t)
	now := time.Unix(1_700_000_000, 0)
	frontier.mu.Lock()
	for runIndex := range runs {
		runID := uuid.New()
		provenance := []byte(fmt.Sprintf("rate-run-%02d", runIndex))
		frontier.state.beginRun(runID, provenance, profile, nil)
		run := frontier.state.runs[runID]
		run.seeding = false
		candidate := frontierCandidate{
			normURL:       fmt.Sprintf("https://rate-%02d.example/page", runIndex),
			host:          fmt.Sprintf("rate-%02d.example", runIndex),
			profileHandle: profile.Profile.Handle,
			provenance:    provenance,
			indexAllowed:  true,
		}
		for range perRun {
			run.appendPending(candidate)
		}
		frontier.rateNextDue[string(provenance)] = now.Add(2 * time.Second)
	}
	frontier.refillReadyLocked()
	frontier.mu.Unlock()
	pace.calls.Store(0)
	_, wait, result := frontier.claimNext(context.Background(), now)
	if result != takeWaiting {
		t.Fatalf("claim result = %d, want waiting", result)
	}
	if wait <= 0 || wait > 2*time.Second {
		t.Fatalf("wait = %s, want bounded future rate", wait)
	}
	if calls := pace.calls.Load(); calls > 2*capacity {
		t.Fatalf("pace probes = %d, want at most %d", calls, 2*capacity)
	}
	frontier.mu.Lock()
	total := len(frontier.state.ready)
	for _, run := range frontier.state.runs {
		total += run.pendingPages
	}
	frontier.mu.Unlock()
	if total != runs*perRun {
		t.Fatalf("retained pages = %d, want %d", total, runs*perRun)
	}
}

func TestCancelDuringEligibilityProbeDoesNotRetainSearchPopulation(t *testing.T) {
	pace := &blockingFuturePace{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	frontier := NewFrontier(1, pace)
	profile := internalProfile(t)
	cancelledProvenance := []byte("cancelled-search")
	survivorProvenance := []byte("surviving-search")
	requests := make([]string, 0, 4096)
	for page := range 4096 {
		requests = append(requests, fmt.Sprintf("https://cancelled.example/page/%04d", page))
	}
	cancelled := frontier.SeedRun(
		context.Background(),
		internalRequests(profile, requests...),
		cancelledProvenance,
		profile,
		nil,
	)
	survivor := frontier.SeedRun(
		context.Background(),
		internalRequests(
			profile,
			"https://survivor.example/one",
			"https://survivor.example/two",
		),
		survivorProvenance,
		profile,
		nil,
	)
	takeContext, stopTake := context.WithCancel(t.Context())
	takeDone := make(chan bool, 1)
	go func() {
		_, ok := frontier.Take(takeContext)
		takeDone <- ok
	}()
	select {
	case <-pace.entered:
	case <-t.Context().Done():
		t.Fatal("eligibility probe did not start")
	}
	cancelStarted := make(chan struct{})
	cancelDone := make(chan struct{})
	go func() {
		close(cancelStarted)
		frontier.Cancel(cancelledProvenance)
		close(cancelDone)
	}()
	<-cancelStarted
	close(pace.release)
	select {
	case <-cancelDone:
	case <-t.Context().Done():
		t.Fatal("cancel did not finish")
	}
	stopTake()
	select {
	case ok := <-takeDone:
		if ok {
			t.Fatal("future-due take unexpectedly claimed work")
		}
	case <-t.Context().Done():
		t.Fatal("take did not stop")
	}
	if calls := pace.calls.Load(); calls > 16 {
		t.Fatalf("pace probes after cancel = %d, want at most 16", calls)
	}
	frontier.mu.Lock()
	_, cancelledRetained := frontier.state.runs[cancelled.RunID]
	survivorPending := frontier.state.completion.Pending(survivor.RunID)
	frontier.mu.Unlock()
	if cancelledRetained {
		t.Fatal("cancelled search population remains retained")
	}
	if survivorPending != 2 {
		t.Fatalf("survivor pending = %d, want 2", survivorPending)
	}
	frontier.Cancel(survivorProvenance)
}

func TestPausedVisibleRunDoesNotHideRunnableOverflow(t *testing.T) {
	frontier := NewFrontier(1, nil)
	profile := internalProfile(t)
	first := []byte("first-run")
	second := []byte("second-run")
	frontier.SeedRun(
		context.Background(),
		internalRequests(profile, "https://first.example/"),
		first,
		profile,
		nil,
	)
	frontier.SeedRun(
		context.Background(),
		internalRequests(profile, "https://second.example/"),
		second,
		profile,
		nil,
	)
	frontier.Pause(first)
	job := internalReceive(t, frontier)
	if string(job.Provenance) != string(second) {
		t.Fatalf("dispatched provenance = %q, want %q", job.Provenance, second)
	}
	frontier.Done(job, successfulPageOutcome())
	frontier.Cancel(first)
}

func TestHostBlockedVisibleJobDoesNotHideOtherHostOverflow(t *testing.T) {
	frontier := NewFrontier(1, nil, WithMaxHostConcurrency(1))
	profile := internalProfile(t)
	provenance := []byte("mixed-hosts")
	frontier.SeedRun(
		context.Background(),
		internalRequests(
			profile,
			"https://first.example/a",
			"https://first.example/b",
			"https://second.example/a",
		),
		provenance,
		profile,
		nil,
	)
	first := internalReceive(t, frontier)
	second := internalReceive(t, frontier)
	if first.URL != "https://first.example/a" {
		t.Fatalf("first job = %q", first.URL)
	}
	if second.URL != "https://second.example/a" {
		t.Fatalf("second job = %q, want other host", second.URL)
	}
	frontier.Done(first, successfulPageOutcome())
	frontier.Done(second, successfulPageOutcome())
	last := internalReceive(t, frontier)
	frontier.Done(last, successfulPageOutcome())
}

func TestHostBlockedReadyWindowsDoNotHideRunnablePendingHost(t *testing.T) {
	frontier := NewFrontier(1, nil, WithMaxHostConcurrency(1))
	profile := internalProfile(t)
	provenance := []byte("deep-mixed-hosts")
	frontier.SeedRun(
		context.Background(),
		internalRequests(
			profile,
			"https://first.example/1",
			"https://first.example/2",
			"https://first.example/3",
			"https://first.example/4",
			"https://first.example/5",
			"https://second.example/1",
		),
		provenance,
		profile,
		nil,
	)
	first := internalReceive(t, frontier)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	second, ok := frontier.Take(ctx)
	if !ok {
		t.Fatal("frontier slept before runnable pending host")
	}
	if second.URL != "https://second.example/1" {
		t.Fatalf("second job = %q, want other host", second.URL)
	}
	frontier.Done(first, successfulPageOutcome())
	frontier.Done(second, successfulPageOutcome())
	frontier.Cancel(provenance)
}

func TestDispatchFairnessPreventsHighScoreRunStarvation(t *testing.T) {
	frontier := NewFrontier(8, nil, WithValueScorer(func(job crawljob.CrawlJob, _ int) float64 {
		if string(job.Provenance) == "high-score" {
			return 100
		}

		return 1
	}))
	profile := internalProfile(t)
	for _, provenance := range []string{"high-score", "low-score"} {
		requests := make([]string, 0, 20)
		for page := range 20 {
			requests = append(
				requests,
				fmt.Sprintf("https://%s.example/page/%02d", provenance, page),
			)
		}
		frontier.SeedRun(
			context.Background(),
			internalRequests(profile, requests...),
			[]byte(provenance),
			profile,
			nil,
		)
	}
	dispatched := map[string]int{"high-score": 0, "low-score": 0}
	for range 20 {
		job := internalReceive(t, frontier)
		dispatched[string(job.Provenance)]++
		difference := dispatched["high-score"] - dispatched["low-score"]
		if difference < -1 || difference > 1 {
			t.Fatalf("dispatch distribution = %v", dispatched)
		}
		frontier.Done(job, successfulPageOutcome())
	}
	frontier.Cancel([]byte("high-score"))
	frontier.Cancel([]byte("low-score"))
}

func TestRefillRotationRepresentsEveryRunnableRunBeyondWindowCapacity(t *testing.T) {
	const runs = 20
	frontier := NewFrontier(1, nil)
	profile := internalProfile(t)
	for run := range runs {
		provenance := []byte(fmt.Sprintf("rotation-%02d", run))
		frontier.SeedRun(
			context.Background(),
			internalRequests(
				profile,
				fmt.Sprintf("https://rotation-%02d.example/one", run),
				fmt.Sprintf("https://rotation-%02d.example/two", run),
			),
			provenance,
			profile,
			nil,
		)
	}
	seen := make(map[string]struct{}, runs)
	for range runs {
		job := internalReceive(t, frontier)
		seen[string(job.Provenance)] = struct{}{}
		frontier.Done(job, successfulPageOutcome())
	}
	if len(seen) != runs {
		t.Fatalf("represented runs = %d, want %d", len(seen), runs)
	}
	for run := range runs {
		frontier.Cancel([]byte(fmt.Sprintf("rotation-%02d", run)))
	}
}

func TestPendingAccountingTracksQueuedAndClaimedJobs(t *testing.T) {
	frontier := NewFrontier(1, nil)
	profile := internalProfile(t)
	provenance := []byte("accounting")
	seeded := frontier.SeedRun(
		context.Background(),
		internalRequests(
			profile,
			"https://example.org/one",
			"https://example.org/two",
			"https://example.org/three",
		),
		provenance,
		profile,
		nil,
	)
	if pending := frontier.RunPending(seeded.RunID); pending != 3 {
		t.Fatalf("pending after seed = %d, want 3", pending)
	}
	job := internalReceive(t, frontier)
	if pending := frontier.RunPending(seeded.RunID); pending != 3 {
		t.Fatalf("pending after claim = %d, want 3", pending)
	}
	frontier.Done(job, successfulPageOutcome())
	if pending := frontier.RunPending(seeded.RunID); pending != 2 {
		t.Fatalf("pending after done = %d, want 2", pending)
	}
	frontier.Cancel(provenance)
	if pending := frontier.RunPending(seeded.RunID); pending != 0 {
		t.Fatalf("pending after cancel = %d, want 0", pending)
	}
}

func TestPendingBackingArrayShrinksBeforeRunCompletion(t *testing.T) {
	const pages = 4096
	frontier := NewFrontier(1, nil)
	profile := internalProfile(t)
	provenance := []byte("compaction")
	requests := make([]string, 0, pages)
	for page := range pages {
		requests = append(requests, fmt.Sprintf("https://example.org/page/%04d", page))
	}
	seeded := frontier.SeedRun(
		context.Background(),
		internalRequests(profile, requests...),
		provenance,
		profile,
		nil,
	)
	frontier.mu.Lock()
	initialCapacity := pendingPageCapacity(frontier.state.runs[seeded.RunID])
	frontier.mu.Unlock()
	claimed := make([]crawljob.CrawlJob, 0, pages-8)
	for range pages - 8 {
		claimed = append(claimed, internalReceive(t, frontier))
	}
	frontier.mu.Lock()
	run := frontier.state.runs[seeded.RunID]
	remaining := run.pendingPages
	currentCapacity := pendingPageCapacity(run)
	frontier.mu.Unlock()
	if remaining >= initialCapacity/4 {
		t.Fatalf("remaining pending = %d, initial capacity = %d", remaining, initialCapacity)
	}
	if currentCapacity >= initialCapacity/2 {
		t.Fatalf("pending capacity = %d, want below half of %d", currentCapacity, initialCapacity)
	}
	frontier.Cancel(provenance)
	for _, job := range claimed {
		frontier.Done(job, successfulPageOutcome())
	}
}

func pendingPageCapacity(run *crawlRun) int {
	capacity := 0
	for _, bucket := range run.pendingHosts {
		if bucket == nil {
			continue
		}
		capacity += cap(bucket.returned)
		capacity += cap(bucket.queued)
	}

	return capacity
}
