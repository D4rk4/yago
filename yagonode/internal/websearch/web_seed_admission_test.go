package websearch

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type blockingWebSeeder struct {
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
	calls    atomic.Int32
	deadline atomic.Bool
}

type deadlineWebSeeder struct {
	remaining chan time.Duration
}

func (s deadlineWebSeeder) Seed(ctx context.Context, _ []string) {
	deadline, ok := ctx.Deadline()
	if !ok {
		s.remaining <- 0

		return
	}
	s.remaining <- time.Until(deadline)
}

func (deadlineWebSeeder) AdmitCrawlSeedURL(rawURL string) (string, bool) {
	return rawURL, rawURL != ""
}

func (s *blockingWebSeeder) Seed(ctx context.Context, _ []string) {
	s.calls.Add(1)
	_, hasDeadline := ctx.Deadline()
	s.deadline.Store(hasDeadline)
	s.started <- struct{}{}
	<-s.release
	s.finished <- struct{}{}
}

func (*blockingWebSeeder) AdmitCrawlSeedURL(rawURL string) (string, bool) {
	return rawURL, rawURL != ""
}

func TestWebSeedCrawlDoesNotDelaySearchResponse(t *testing.T) {
	seeder := newBlockingWebSeeder()
	searcher := NewFallbackSearcher(
		&stubSearcher{},
		&stubProvider{results: []Result{{Title: "gap", URL: "https://web.example/gap"}}},
		enabled,
		WithSeeder(seeder),
	)
	searchDone := make(chan error, 1)
	go func() {
		_, err := searcher.Search(t.Context(), searchcore.Request{Query: "gap", Limit: 10})
		searchDone <- err
	}()

	waitForWebSeedSignal(t, seeder.started, "seeder did not start")
	select {
	case err := <-searchDone:
		if err != nil {
			t.Fatalf("search: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("search waited for crawl seeding")
	}
	if !seeder.deadline.Load() {
		t.Fatal("background seeding has no deadline")
	}
	close(seeder.release)
	waitForWebSeedSignal(t, seeder.finished, "seeder did not finish")
}

func TestWebSeedCrawlCoalescesDuplicateURLsWhileWorkerIsBusy(t *testing.T) {
	seeder := newBlockingWebSeeder()
	admission := newWebSeedAdmission(1)
	searcher := NewFallbackSearcher(
		&stubSearcher{},
		&stubProvider{results: []Result{{Title: "gap", URL: "https://web.example/gap"}}},
		enabled,
		WithSeeder(seeder),
	)
	searcher.spawnSeedWork = admission.try

	if _, err := searcher.Search(
		t.Context(),
		searchcore.Request{Query: "gap", Limit: 10},
	); err != nil {
		t.Fatalf("first search: %v", err)
	}
	waitForWebSeedSignal(t, seeder.started, "first seeder did not start")
	if _, err := searcher.Search(
		t.Context(),
		searchcore.Request{Query: "gap", Limit: 10},
	); err != nil {
		t.Fatalf("saturated search: %v", err)
	}
	if seeder.calls.Load() != 1 {
		t.Fatalf("seeder calls = %d, want 1", seeder.calls.Load())
	}
	close(seeder.release)
	waitForWebSeedSignal(t, seeder.finished, "first seeder did not finish")
	waitForWebSeedAdmissionRelease(t, admission, "https://web.example/gap")
	if seeder.calls.Load() != 1 {
		t.Fatalf("seeder calls = %d, want 1 coalesced URL", seeder.calls.Load())
	}
	if _, err := searcher.Search(
		t.Context(),
		searchcore.Request{Query: "gap", Limit: 10},
	); err != nil {
		t.Fatalf("retry search: %v", err)
	}
	waitForWebSeedSignal(t, seeder.started, "retry seeder did not start")
	waitForWebSeedSignal(t, seeder.finished, "retry seeder did not finish")
	if seeder.calls.Load() != 2 {
		t.Fatalf("seeder calls after completion = %d, want 2", seeder.calls.Load())
	}
}

func TestWebSeedAdmissionQueuesDistinctURLWhenWorkerIsBusy(t *testing.T) {
	admission := newWebSeedAdmission(1)
	started := make(chan struct{})
	release := make(chan struct{})
	if !admission.try("active", t.Context(), func(context.Context) {
		close(started)
		<-release
	}) {
		t.Fatal("active URL was rejected")
	}
	<-started
	queued := make(chan struct{})
	if !admission.try("queued", t.Context(), func(context.Context) { close(queued) }) {
		t.Fatal("distinct queued URL was rejected")
	}
	select {
	case <-queued:
		t.Fatal("queued URL started while the worker was busy")
	default:
	}
	close(release)
	waitForWebSeedSignal(t, queued, "queued URL did not start")
}

func TestWebSeedAdmissionBoundsPendingWork(t *testing.T) {
	admission := newWebSeedAdmission(1)
	started := make(chan struct{})
	release := make(chan struct{})
	if !admission.try("active", t.Context(), func(context.Context) {
		close(started)
		<-release
	}) {
		t.Fatal("active work was rejected")
	}
	<-started
	for index := 0; index < cap(admission.pending); index++ {
		if !admission.try(
			fmt.Sprintf("pending-%d", index),
			t.Context(),
			func(context.Context) {},
		) {
			t.Fatalf("pending work %d was rejected", index)
		}
	}
	if admission.try("beyond", t.Context(), func(context.Context) {}) {
		t.Fatal("work beyond the bounded pending queue was accepted")
	}
	close(release)
}

func TestWebSeedCrawlDoesNotStartRejectedWork(t *testing.T) {
	seeder := &stubSeeder{}
	searcher := &FallbackSearcher{
		seeder: seeder,
		spawnSeedWork: func(string, context.Context, func(context.Context)) bool {
			return false
		},
	}
	searcher.seedWebResults(t.Context(), []Result{{URL: "https://web.example/fresh"}})
	if seeder.calls != 0 {
		t.Fatalf("seeder calls = %d", seeder.calls)
	}
}

func TestQueuedWebSeedWorkReceivesItsFullExecutionBudget(t *testing.T) {
	admission := newWebSeedAdmission(1)
	started := make(chan struct{})
	release := make(chan struct{})
	if !admission.try("blocking", t.Context(), func(context.Context) {
		close(started)
		<-release
	}) {
		t.Fatal("blocking work was rejected")
	}
	<-started
	remaining := make(chan time.Duration, 1)
	searcher := &FallbackSearcher{
		seeder:        deadlineWebSeeder{remaining: remaining},
		spawnSeedWork: admission.try,
	}
	searcher.seedWebResults(t.Context(), []Result{{URL: "https://web.example/fresh"}})
	time.Sleep(100 * time.Millisecond)
	close(release)
	select {
	case got := <-remaining:
		if got < webSeedWriteTimeout-500*time.Millisecond {
			t.Fatalf("queued seed budget = %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("queued seeding did not start")
	}
}

func TestWebSeedWorkDoesNotRetainRequestContextValues(t *testing.T) {
	type requestValueKey struct{}
	admission := newWebSeedAdmission(1)
	observed := make(chan any, 1)
	requestContext := context.WithValue(t.Context(), requestValueKey{}, "request state")
	if !admission.try("isolated", requestContext, func(ctx context.Context) {
		observed <- ctx.Value(requestValueKey{})
	}) {
		t.Fatal("isolated work was rejected")
	}
	select {
	case value := <-observed:
		if value != nil {
			t.Fatalf("background work retained request value %#v", value)
		}
	case <-time.After(time.Second):
		t.Fatal("isolated work did not run")
	}
}

func TestWebSeedAdmissionRecoversWorkerPanicAndReleasesURL(t *testing.T) {
	admission := newWebSeedAdmission(1)
	if !admission.try("panic", t.Context(), func(context.Context) { panic("seed failure") }) {
		t.Fatal("panicking URL was rejected")
	}
	waitForWebSeedAdmissionRelease(t, admission, "panic")
	continued := make(chan struct{})
	if !admission.try("continued", t.Context(), func(context.Context) { close(continued) }) {
		t.Fatal("work after panic was rejected")
	}
	waitForWebSeedSignal(t, continued, "worker did not continue after panic")
}

func newBlockingWebSeeder() *blockingWebSeeder {
	return &blockingWebSeeder{
		started:  make(chan struct{}, 2),
		release:  make(chan struct{}),
		finished: make(chan struct{}, 2),
	}
}

func waitForWebSeedSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(failure)
	}
}

func waitForWebSeedAdmissionRelease(
	t *testing.T,
	admission *webSeedAdmission,
	key string,
) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		admission.mutex.Lock()
		_, admitted := admission.admitted[key]
		admission.mutex.Unlock()
		if !admitted {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("web seed URL %q remained admitted", key)
}
