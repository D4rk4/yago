package main

import (
	"context"
	"testing"
	"time"
)

type fakeCrawlWorker struct {
	started            chan struct{}
	blockUntilFetchEnd bool
}

func (w *fakeCrawlWorker) RunWorkers(acceptCtx, fetchCtx context.Context, _ int) {
	close(w.started)
	if w.blockUntilFetchEnd {
		<-fetchCtx.Done()

		return
	}
	<-acceptCtx.Done()
}

type fakeOrderConsumer struct {
	suspendActive func()
	wait          func()
}

type resizingCrawlWorker struct {
	runs    chan int
	release chan struct{}
	calls   int
}

func (w *resizingCrawlWorker) RunWorkers(
	acceptCtx,
	fetchCtx context.Context,
	workers int,
) {
	w.calls++
	w.runs <- workers
	<-acceptCtx.Done()
	if w.calls == 1 {
		select {
		case <-fetchCtx.Done():
			return
		case <-w.release:
		}
	}
}

func (fakeOrderConsumer) Run(ctx context.Context) { <-ctx.Done() }

func (c fakeOrderConsumer) SuspendActiveRuns() {
	if c.suspendActive != nil {
		c.suspendActive()
	}
}

func (c fakeOrderConsumer) WaitForSettlements() {
	if c.wait != nil {
		c.wait()
	}
}

func runSupervise(
	t *testing.T,
	worker crawlWorker,
	started chan struct{},
	grace time.Duration,
) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		superviseCrawl(ctx, worker, fakeOrderConsumer{}, 1, grace)
		close(done)
	}()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("superviseCrawl did not return after shutdown")
	}
}

func TestSuperviseCrawlStopsWorkersOnSignal(t *testing.T) {
	worker := &fakeCrawlWorker{started: make(chan struct{})}
	runSupervise(t, worker, worker.started, time.Second)
}

func TestSuperviseCrawlAbortsInFlightAfterGrace(t *testing.T) {
	worker := &fakeCrawlWorker{started: make(chan struct{}), blockUntilFetchEnd: true}
	runSupervise(t, worker, worker.started, 50*time.Millisecond)
}

func TestSuperviseCrawlSuspendsRunsAndWaitsForSettlement(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	worker := &fakeCrawlWorker{started: make(chan struct{})}
	activeSuspended := make(chan struct{})
	releaseSettlement := make(chan struct{})
	consumer := fakeOrderConsumer{
		suspendActive: func() { close(activeSuspended) },
		wait:          func() { <-releaseSettlement },
	}
	done := make(chan struct{})
	go func() {
		superviseCrawl(ctx, worker, consumer, 1, time.Second)
		close(done)
	}()
	<-worker.started
	cancel()
	select {
	case <-activeSuspended:
	case <-time.After(time.Second):
		t.Fatal("active runs were not suspended during shutdown")
	}
	select {
	case <-done:
		t.Fatal("supervisor returned before order settlement")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseSettlement)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not return after order settlement")
	}
}

func TestSuperviseCrawlResizesAfterInflightFetchesDrain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	control := newWorkerConcurrency(1)
	worker := &resizingCrawlWorker{
		runs:    make(chan int, 3),
		release: make(chan struct{}),
	}
	done := make(chan struct{})
	go func() {
		superviseCrawlWithConcurrency(
			ctx,
			worker,
			fakeOrderConsumer{},
			control,
			time.Second,
		)
		close(done)
	}()
	if got := <-worker.runs; got != 1 {
		t.Fatalf("initial workers = %d, want 1", got)
	}
	control.Set(3)
	control.Set(5)
	select {
	case got := <-worker.runs:
		t.Fatalf("workers restarted at %d before in-flight work drained", got)
	case <-time.After(20 * time.Millisecond):
	}
	close(worker.release)
	select {
	case got := <-worker.runs:
		if got != 5 {
			t.Fatalf("resized workers = %d, want latest value 5", got)
		}
	case <-time.After(time.Second):
		t.Fatal("workers did not restart after in-flight work drained")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("resizable supervisor did not stop")
	}
}
