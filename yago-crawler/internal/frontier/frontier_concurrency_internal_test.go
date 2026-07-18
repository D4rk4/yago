package frontier

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func internalProfile(t *testing.T) crawladmission.AdmissionProfile {
	t.Helper()
	profile, err := crawladmission.CompileProfile(yagocrawlcontract.NewCrawlProfile(
		yagocrawlcontract.CrawlProfile{
			Scope:           yagocrawlcontract.ScopeWide,
			URLMustMatch:    yagocrawlcontract.MatchAll,
			MaxDepth:        2,
			MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
		},
	))
	if err != nil {
		t.Fatalf("compile profile: %v", err)
	}

	return profile
}

func internalRequests(
	profile crawladmission.AdmissionProfile,
	urls ...string,
) []yagocrawlcontract.CrawlRequest {
	requests := make([]yagocrawlcontract.CrawlRequest, 0, len(urls))
	for _, rawURL := range urls {
		requests = append(requests, yagocrawlcontract.CrawlRequest{
			URL:           rawURL,
			ProfileHandle: profile.Profile.Handle,
		})
	}

	return requests
}

func internalReceive(t *testing.T, frontier *Frontier) crawljob.CrawlJob {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if job, ok := frontier.Take(ctx); ok {
		return job
	}
	t.Fatal("timed out waiting for crawl job")

	return crawljob.CrawlJob{}
}

func TestCompletedRunsReleaseFrontierAndRateState(t *testing.T) {
	frontier := NewFrontier(8, nil)
	profile := internalProfile(t)
	provenance := []byte("shared-provenance")
	frontier.SetRate(provenance, 60_000)
	first := frontier.SeedRun(
		context.Background(),
		internalRequests(profile, "https://one.example/"),
		provenance,
		profile,
		nil,
	)
	second := frontier.SeedRun(
		context.Background(),
		internalRequests(profile, "https://two.example/"),
		provenance,
		profile,
		nil,
	)
	firstJob := internalReceive(t, frontier)
	secondJob := internalReceive(t, frontier)
	frontier.Pause(provenance)

	frontier.Done(firstJob, successfulPageOutcome())
	frontier.mu.Lock()
	retainedRuns := len(frontier.state.runs)
	if retainedRuns != 1 {
		frontier.mu.Unlock()
		t.Fatalf("retained runs after first completion = %d, want 1", retainedRuns)
	}
	if _, exists := frontier.rateInterval[string(provenance)]; !exists {
		frontier.mu.Unlock()
		t.Fatal("shared rate state was removed while another run remained active")
	}
	frontier.mu.Unlock()

	frontier.Done(secondJob, successfulPageOutcome())
	frontier.mu.Lock()
	defer frontier.mu.Unlock()
	if _, exists := frontier.state.runs[first.RunID]; exists {
		t.Fatal("first completed run remains retained")
	}
	if _, exists := frontier.state.runs[second.RunID]; exists {
		t.Fatal("second completed run remains retained")
	}
	if _, exists := frontier.rateInterval[string(provenance)]; exists {
		t.Fatal("completed run rate interval remains retained")
	}
	if _, exists := frontier.rateNextDue[string(provenance)]; exists {
		t.Fatal("completed run next-due state remains retained")
	}
	if _, exists := frontier.paused[string(provenance)]; exists {
		t.Fatal("completed run pause state remains retained")
	}
}

type reservationPace struct {
	visited chan struct{}
	once    sync.Once
}

func (p *reservationPace) DueAt(crawljob.CrawlJob, time.Time) time.Time {
	return time.Time{}
}

func (p *reservationPace) Visited(crawljob.CrawlJob, time.Time) {
	p.once.Do(func() { close(p.visited) })
}

func TestCancelBeforeTakeSettlesWithoutClaim(t *testing.T) {
	pace := &reservationPace{visited: make(chan struct{})}
	frontier := NewFrontier(0, pace, WithMaxHostConcurrency(1))
	profile := internalProfile(t)
	provenance := []byte("cancel-race")
	finished := make(chan struct{})
	frontier.SeedRun(
		context.Background(),
		internalRequests(profile, "https://race.example/"),
		provenance,
		profile,
		func(bool) { close(finished) },
	)

	frontier.Cancel(provenance)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled reservation did not settle")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if job, ok := frontier.Take(ctx); ok {
		t.Fatalf("cancelled run returned %q", job.URL)
	}
	select {
	case <-pace.visited:
		t.Fatal("cancelled queued job was recorded as visited")
	default:
	}
	frontier.mu.Lock()
	defer frontier.mu.Unlock()
	if len(frontier.inflight) != 0 {
		t.Fatalf("in-flight host slots after immediate completion = %v", frontier.inflight)
	}
	if len(frontier.state.runs) != 0 {
		t.Fatalf("retained runs after concurrent cancel = %d", len(frontier.state.runs))
	}
}

func TestTakeBeforeCancelLeavesClaimedJobForDone(t *testing.T) {
	pace := &reservationPace{visited: make(chan struct{})}
	frontier := NewFrontier(1, pace, WithMaxHostConcurrency(1))
	profile := internalProfile(t)
	provenance := []byte("claimed-cancel")
	finished := make(chan struct{})
	frontier.SeedRun(
		context.Background(),
		internalRequests(profile, "https://race.example/"),
		provenance,
		profile,
		func(bool) { close(finished) },
	)
	job := internalReceive(t, frontier)
	frontier.Cancel(provenance)
	select {
	case <-finished:
		t.Fatal("claimed job settled before Done")
	case <-time.After(20 * time.Millisecond):
	}
	frontier.Done(job, successfulPageOutcome())
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("claimed cancelled job did not settle after Done")
	}
}

func TestTakeWithCancelledContextDoesNotClaimDueWork(t *testing.T) {
	pace := &reservationPace{visited: make(chan struct{})}
	frontier := NewFrontier(2, pace)
	profile := internalProfile(t)
	provenance := []byte("stopped-intake")
	frontier.SeedRun(
		context.Background(),
		internalRequests(profile, "https://one.example/", "https://two.example/"),
		provenance,
		profile,
		nil,
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if job, ok := frontier.Take(ctx); ok {
		t.Fatalf("cancelled intake claimed %q", job.URL)
	}
	select {
	case <-pace.visited:
		t.Fatal("cancelled intake recorded a visit")
	default:
	}
	frontier.Cancel(provenance)
}

func TestCancellationRemainsVisibleUntilAllSharedProvenanceRunsFinish(t *testing.T) {
	frontier := NewFrontier(2, nil)
	profile := internalProfile(t)
	provenance := []byte("shared-cancel")
	results := make(chan bool, 2)
	finish := func(bool) {
		cancelled := frontier.WasCancelled(provenance)
		frontier.ClearCancelled(provenance)
		results <- cancelled
	}
	frontier.SeedRun(
		context.Background(),
		internalRequests(profile, "https://one.example/"),
		provenance,
		profile,
		finish,
	)
	frontier.SeedRun(
		context.Background(),
		internalRequests(profile, "https://two.example/"),
		provenance,
		profile,
		finish,
	)
	first := internalReceive(t, frontier)
	second := internalReceive(t, frontier)
	frontier.Cancel(provenance)
	frontier.Done(first, successfulPageOutcome())
	if cancelled := <-results; !cancelled {
		t.Fatal("first shared run lost its cancellation state")
	}
	if !frontier.WasCancelled(provenance) {
		t.Fatal("first completion cleared the second run cancellation")
	}
	frontier.Done(second, successfulPageOutcome())
	if cancelled := <-results; !cancelled {
		t.Fatal("second shared run lost its cancellation state")
	}
	if frontier.WasCancelled(provenance) {
		t.Fatal("cancellation state remained after both runs completed")
	}
}

func TestPendingControlsStayBoundedBeforeRunRegistration(t *testing.T) {
	frontier := NewFrontier(1, nil)
	for index := 0; index <= pendingControlLimit; index++ {
		frontier.Pause([]byte(fmt.Sprintf("pending-%05d", index)))
	}
	frontier.mu.Lock()
	defer frontier.mu.Unlock()
	if len(frontier.controlSeen) != pendingControlLimit {
		t.Fatalf("pending controls = %d, want %d", len(frontier.controlSeen), pendingControlLimit)
	}
	if len(frontier.paused) != pendingControlLimit {
		t.Fatalf("pending pauses = %d, want %d", len(frontier.paused), pendingControlLimit)
	}
}

func TestCandidatePreparationDoesNotHoldFrontierMutationLock(t *testing.T) {
	frontier := NewFrontier(8, nil)
	profile := internalProfile(t)
	seeded := frontier.SeedRun(
		context.Background(),
		internalRequests(profile, "https://source.example/"),
		[]byte("link-preparation"),
		profile,
		nil,
	)
	job := internalReceive(t, frontier)
	preparationStarted := make(chan struct{})
	releasePreparation := make(chan struct{})
	frontier.prepareLinks = func(
		crawljob.CrawlJob,
		crawljob.DiscoveredLinks,
		crawladmission.AdmissionProfile,
	) []frontierCandidate {
		close(preparationStarted)
		<-releasePreparation

		return nil
	}
	submitDone := make(chan struct{})
	go func() {
		frontier.Submit(context.Background(), job, crawljob.DiscoveredLinks{
			Followable: []string{"https://target.example/"},
		})
		close(submitDone)
	}()
	<-preparationStarted
	controlDone := make(chan struct{})
	go func() {
		frontier.SetRate(job.Provenance, 0)
		close(controlDone)
	}()
	select {
	case <-controlDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("frontier mutation lock was held during link preparation")
	}
	close(releasePreparation)
	<-submitDone
	frontier.Done(job, successfulPageOutcome())
	if pending := frontier.RunPending(seeded.RunID); pending != 0 {
		t.Fatalf("pending after completed preparation test = %d, want 0", pending)
	}
}

func TestSeedPreparationDoesNotHoldFrontierMutationLock(t *testing.T) {
	frontier := NewFrontier(1, nil)
	profile := internalProfile(t)
	provenance := []byte("seed-preparation")
	preparationStarted := make(chan struct{})
	releasePreparation := make(chan struct{})
	frontier.prepareSeeds = func(
		context.Context,
		[]yagocrawlcontract.CrawlRequest,
		[]byte,
		crawladmission.AdmissionProfile,
	) []frontierCandidate {
		close(preparationStarted)
		<-releasePreparation

		return nil
	}
	seedDone := make(chan struct{})
	go func() {
		frontier.SeedRun(
			context.Background(),
			internalRequests(profile, "https://seed.example/"),
			provenance,
			profile,
			nil,
		)
		close(seedDone)
	}()
	<-preparationStarted
	controlDone := make(chan struct{})
	go func() {
		frontier.SetRate(provenance, 0)
		close(controlDone)
	}()
	select {
	case <-controlDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("frontier mutation lock was held during seed preparation")
	}
	close(releasePreparation)
	<-seedDone
}

func TestAcceptRejectsMissingProfileOnKnownRun(t *testing.T) {
	frontier := NewFrontier(1, nil)
	profile := internalProfile(t)
	runID := uuid.New()
	frontier.state.beginRun(runID, nil, profile, nil)
	accepted, _ := frontier.state.accept(context.Background(), runID, frontierCandidate{
		normURL:       "https://example.com/",
		profileHandle: "missing",
	})
	if accepted {
		t.Fatal("candidate with a missing profile was accepted")
	}
}

func TestSeedCandidatesStayUndispatchableUntilAdmissionCompletes(t *testing.T) {
	frontier := NewFrontier(1, nil)
	profile := internalProfile(t)
	runID := uuid.New()
	frontier.mu.Lock()
	frontier.state.beginRun(runID, []byte("seeding"), profile, nil)
	frontier.state.accept(context.Background(), runID, preparedCandidate(
		frontierCandidateSource{
			normalized:    "https://example.com/",
			profileHandle: profile.Profile.Handle,
			provenance:    []byte("seeding"),
		},
		profile,
	))
	if frontier.nextDue(time.Now()).due {
		frontier.mu.Unlock()
		t.Fatal("seed candidate became dispatchable before seed admission completed")
	}
	frontier.state.runs[runID].seeding = false
	if !frontier.nextDue(time.Now()).due {
		frontier.mu.Unlock()
		t.Fatal("seed candidate remained withheld after seed admission completed")
	}
	frontier.mu.Unlock()
}
