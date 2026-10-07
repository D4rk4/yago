package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/tracectx"
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
	admission := newWebSeedAdmission(1, webSeedPendingPerWorker)
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
	admission := newWebSeedAdmission(1, webSeedPendingPerWorker)
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
	admission := newWebSeedAdmission(1, webSeedPendingPerWorker)
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
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	const traceID = "11111111111111112222222222222222"
	const parentSpanID = "3333333333333333"
	ctx, _ := tracectx.StartServerSpan(
		t.Context(),
		"00-"+traceID+"-"+parentSpanID+"-01",
	)
	serverSpanID, ok := tracectx.ServerSpanIDFromContext(ctx)
	if !ok {
		t.Fatal("server span ID missing")
	}
	seeder := &stubSeeder{}
	searcher := &FallbackSearcher{
		primary: &stubSearcher{},
		provider: &stubProvider{results: []Result{{
			Title: "synthetic search result",
			URL:   "https://private.example/PRIVATE_URL_CANARY",
		}}},
		permit: enabled,
		seeder: seeder,
		spawnSeedWork: func(string, context.Context, func(context.Context)) bool {
			return false
		},
	}
	if _, err := searcher.Search(ctx, searchcore.Request{
		Query: "PRIVATE_QUERY_CANARY",
		Limit: 1,
	}); err != nil {
		t.Fatalf("search: %v", err)
	}
	if seeder.calls != 0 {
		t.Fatalf("seeder calls = %d", seeder.calls)
	}
	assertWebSeedAdmissionLogs(t, output.String(), serverSpanID, traceID, parentSpanID)
}

func assertWebSeedAdmissionLogs(
	t *testing.T,
	output, serverSpanID, traceID, parentSpanID string,
) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 2 {
		t.Fatalf("seed log count = %d, want considered and rejected", len(lines))
	}
	seen := map[string]bool{}
	for _, line := range lines {
		entry := map[string]any{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode seed log: %v", err)
		}
		message, _ := entry["msg"].(string)
		seen[message] = true
		if entry["serverSpanId"] != serverSpanID {
			t.Errorf("%s serverSpanId = %v, want %s", message, entry["serverSpanId"], serverSpanID)
		}
		switch message {
		case msgWebSeedConsidered:
			if entry["results"] != float64(1) || entry["admitted"] != float64(1) {
				t.Errorf("considered counts = %#v", entry)
			}
		case msgWebSeedRejected:
			if entry["urls"] != float64(1) {
				t.Errorf("rejected counts = %#v", entry)
			}
		}
	}
	if !seen[msgWebSeedConsidered] || !seen[msgWebSeedRejected] {
		t.Fatalf("seed log messages = %v", seen)
	}
	assertWebSeedLogsRedact(t, output,
		"PRIVATE_QUERY_CANARY",
		"PRIVATE_URL_CANARY",
		traceID,
		parentSpanID,
	)
}

func assertWebSeedLogsRedact(t *testing.T, output string, canaries ...string) {
	t.Helper()
	for _, canary := range canaries {
		if strings.Contains(output, canary) {
			t.Errorf("seed logs contain private canary %q", canary)
		}
	}
}

func TestQueuedWebSeedWorkReceivesItsFullExecutionBudget(t *testing.T) {
	admission := newWebSeedAdmission(1, webSeedPendingPerWorker)
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

func TestWebSeedWorkCopiesOnlyTrustedServerSpan(t *testing.T) {
	type requestValueKey struct{}
	type observedContext struct {
		requestValue any
		serverSpan   string
		spanPresent  bool
		tracePresent bool
		deadline     bool
		err          error
	}
	admission := newWebSeedAdmission(1, 2)
	blockerStarted := make(chan struct{})
	releaseBlocker := make(chan struct{})
	if !admission.try("blocker", context.Background(), func(context.Context) {
		close(blockerStarted)
		<-releaseBlocker
	}) {
		t.Fatal("blocking work was rejected")
	}
	waitForWebSeedSignal(t, blockerStarted, "blocking work did not start")
	parentContext, _ := tracectx.StartServerSpan(
		t.Context(),
		"00-11111111111111112222222222222222-3333333333333333-01",
	)
	requestContext, cancel := context.WithCancel(
		context.WithValue(parentContext, requestValueKey{}, "request state"),
	)
	expectedServerSpanID, ok := tracectx.ServerSpanIDFromContext(requestContext)
	if !ok {
		t.Fatal("request server span ID missing")
	}
	observed := make(chan observedContext, 1)
	if !admission.try("isolated", requestContext, func(ctx context.Context) {
		_, tracePresent := tracectx.FromContext(ctx)
		serverSpanID, spanPresent := tracectx.ServerSpanIDFromContext(ctx)
		_, deadline := ctx.Deadline()
		observed <- observedContext{
			requestValue: ctx.Value(requestValueKey{}),
			serverSpan:   serverSpanID,
			spanPresent:  spanPresent,
			tracePresent: tracePresent,
			deadline:     deadline,
			err:          ctx.Err(),
		}
	}) {
		t.Fatal("isolated work was rejected")
	}
	cancel()
	close(releaseBlocker)
	select {
	case got := <-observed:
		if got.requestValue != nil || !got.spanPresent || got.serverSpan != expectedServerSpanID ||
			got.tracePresent || !got.deadline || got.err != nil {
			t.Fatalf("background context = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("isolated work did not run")
	}
}

func TestWebSeedAdmissionRecoversWorkerPanicAndReleasesURL(t *testing.T) {
	admission := newWebSeedAdmission(1, webSeedPendingPerWorker)
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

// The warming queue holds whole answers: one fallback answer submits up to
// MaxResults URLs together, so a capacity fixed independently of MaxResults
// dropped seeds as soon as a few queries overlapped. The largest requested size
// wins, and a smaller request never shrinks it.
func TestSizeSeedAdmissionKeepsTheLargestRequest(t *testing.T) {
	SizeSeedAdmission(10)
	if got := webSeedAdmissionSize.Load(); got != 10*webSeedResultSetsQueued {
		t.Fatalf("admission size = %d", got)
	}
	SizeSeedAdmission(4)
	if got := webSeedAdmissionSize.Load(); got != 10*webSeedResultSetsQueued {
		t.Fatalf("a smaller request shrank the queue to %d", got)
	}
	SizeSeedAdmission(40)
	if got := webSeedAdmissionSize.Load(); got != 40*webSeedResultSetsQueued {
		t.Fatalf("a larger request did not grow the queue: %d", got)
	}
	if admission := webSeedProcessAdmission(); admission == nil ||
		admission != webSeedProcessAdmission() {
		t.Fatal("the process warming queue is not a stable singleton")
	}
}
