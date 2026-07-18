package pipeline_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yago-crawler/internal/ingest"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yago-crawler/internal/pageindex"
	"github.com/D4rk4/yago/yago-crawler/internal/pipeline"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

type spyEmitter struct {
	mu         sync.Mutex
	emits      int
	removals   []ingest.Envelope
	removalErr error
	started    chan struct{}
	release    chan struct{}
	startOnce  sync.Once
}

func (e *spyEmitter) Emit(
	context.Context,
	yagocrawlcontract.DocumentIngest,
	[]yagomodel.RWIPosting,
	yagomodel.URIMetadataRow,
	ingest.Envelope,
) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.emits++

	return nil
}

func (e *spyEmitter) EmitRemoval(ctx context.Context, envelope ingest.Envelope) error {
	if e.started != nil {
		e.startOnce.Do(func() { close(e.started) })
	}
	if e.release != nil {
		select {
		case <-e.release:
		case <-ctx.Done():
			return fmt.Errorf("emit test removal: %w", ctx.Err())
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.removals = append(e.removals, envelope)

	return e.removalErr
}

func (e *spyEmitter) removalEnvelopes() []ingest.Envelope {
	e.mu.Lock()
	defer e.mu.Unlock()

	return append([]ingest.Envelope(nil), e.removals...)
}

func (e *spyEmitter) emitCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.emits
}

func TestPipelineTombstonesGonePage(t *testing.T) {
	frontier := newRecordingFrontier()
	emitter := &spyEmitter{}
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, &pagefetch.GoneError{Status: http.StatusNotFound}
		}),
		pageindex.NewIndexBuilder(),
		emitter,
	)
	done := runOneJob(t, p, frontier)
	if done.outcome.Failed != 1 {
		t.Fatalf("gone-page tally = %+v, want one failed fetch", done.outcome)
	}
	if removals := emitter.removalEnvelopes(); len(removals) != 1 ||
		removals[0].SourceURL != "https://example.com/" {
		t.Fatalf("removals = %v, want the job URL", removals)
	}
	if emitter.emitCount() != 0 {
		t.Errorf("a gone page must not emit a document, emits = %d", emitter.emitCount())
	}
}

func TestPipelineMarksDeliveryFailedOnRemovalEmitError(t *testing.T) {
	frontier := newRecordingFrontier()
	emitter := &spyEmitter{removalErr: errors.New("emit failed")}
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, &pagefetch.GoneError{Status: http.StatusGone}
		}),
		pageindex.NewIndexBuilder(),
		emitter,
	)
	if done := runOneJob(t, p, frontier); done.outcome.Failed != 1 {
		t.Fatalf("removal failure tally = %+v, want one failed page", done.outcome)
	}
}

func TestPipelineAbandonsGonePageWhenRemovalIsCancelled(t *testing.T) {
	frontier := newRecordingFrontier()
	emitter := &spyEmitter{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, &pagefetch.GoneError{Status: http.StatusGone}
		}),
		pageindex.NewIndexBuilder(),
		emitter,
	)
	ctx, cancel := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() {
		p.RunWorkers(ctx, ctx, 1)
		close(workerDone)
	}()
	job := crawljob.CrawlJob{
		URL:           "https://example.com/",
		ProfileHandle: "profile",
		ObservationID: "stable-observation",
	}
	frontier.jobs <- job
	select {
	case <-emitter.started:
	case <-time.After(time.Second):
		t.Fatal("removal emit did not start")
	}
	cancel()
	select {
	case abandoned := <-frontier.abandoned:
		if abandoned.ObservationID != job.ObservationID {
			t.Fatalf(
				"abandoned observation = %q, want %q",
				abandoned.ObservationID,
				job.ObservationID,
			)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled removal did not abandon its checkpoint page")
	}
	select {
	case done := <-frontier.done:
		t.Fatalf("cancelled removal completed page: %+v", done)
	default:
	}
	select {
	case <-workerDone:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after removal cancellation")
	}
}

func TestPipelineDoesNotTombstoneOnGenericRejection(t *testing.T) {
	frontier := newRecordingFrontier()
	emitter := &spyEmitter{}
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, fmt.Errorf("status 500: %w", pagefetch.ErrPageRejected)
		}),
		pageindex.NewIndexBuilder(),
		emitter,
	)
	done := runOneJob(t, p, frontier)
	if done.outcome.Failed != 1 {
		t.Fatalf("rejected-page tally = %+v, want one failed fetch", done.outcome)
	}
	if removals := emitter.removalEnvelopes(); len(removals) != 0 {
		t.Fatalf("a non-gone rejection must not emit a removal, got %v", removals)
	}
}

func TestPipelineAbandonsFetchWhenLeaseExpires(t *testing.T) {
	frontier := newRecordingFrontier()
	fetchStarted := make(chan struct{})
	fetchCause := make(chan error, 1)
	registry := confirmedLeaseRegistry(t, "lease-expiry")
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(ctx context.Context, _ *url.URL) (pagefetch.FetchedPage, error) {
			close(fetchStarted)
			<-ctx.Done()
			fetchCause <- context.Cause(ctx)

			return pagefetch.FetchedPage{}, ctx.Err()
		}),
		pageindex.NewIndexBuilder(),
		&spyEmitter{},
		pipeline.WithLeaseGrants(registry),
	)
	acceptContext, cancelAccept := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() {
		p.RunWorkers(acceptContext, t.Context(), 1)
		close(workerDone)
	}()
	job := crawljob.CrawlJob{
		URL: "https://example.com/", ProfileHandle: "profile",
		ObservationID: "lease-observation", LeaseID: "lease-expiry",
	}
	frontier.jobs <- job
	select {
	case <-fetchStarted:
	case <-time.After(time.Second):
		t.Fatal("lease-bound fetch did not start")
	}
	registry.Revoke(job.LeaseID)
	select {
	case cause := <-fetchCause:
		if !errors.Is(cause, crawllease.ErrLeaseLost) {
			t.Fatalf("fetch cancellation cause = %v", cause)
		}
	case <-time.After(time.Second):
		t.Fatal("lease loss did not cancel fetch")
	}
	select {
	case abandoned := <-frontier.abandoned:
		if abandoned.ObservationID != job.ObservationID {
			t.Fatalf("abandoned observation = %q", abandoned.ObservationID)
		}
	case <-time.After(time.Second):
		t.Fatal("lease loss did not abandon checkpoint page")
	}
	select {
	case done := <-frontier.done:
		t.Fatalf("lease-lost page completed: %+v", done)
	default:
	}
	cancelAccept()
	select {
	case <-workerDone:
	case <-time.After(time.Second):
		t.Fatal("lease-lost worker did not stop")
	}
}

func confirmedLeaseRegistry(t *testing.T, leaseID string) *crawllease.GrantRegistry {
	t.Helper()
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	if err := registry.Track(leaseID); err != nil {
		t.Fatalf("track lease: %v", err)
	}
	started := time.Now()
	registry.Renew(started, time.Hour, []string{leaseID}, []string{leaseID})

	return registry
}
