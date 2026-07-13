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
	cancelActive func()
	wait         func()
}

func (fakeOrderConsumer) Run(ctx context.Context) { <-ctx.Done() }

func (c fakeOrderConsumer) CancelActiveRuns() {
	if c.cancelActive != nil {
		c.cancelActive()
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

func TestSuperviseCrawlCancelsRunsAndWaitsForSettlement(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	worker := &fakeCrawlWorker{started: make(chan struct{})}
	activeCancelled := make(chan struct{})
	releaseSettlement := make(chan struct{})
	consumer := fakeOrderConsumer{
		cancelActive: func() { close(activeCancelled) },
		wait:         func() { <-releaseSettlement },
	}
	done := make(chan struct{})
	go func() {
		superviseCrawl(ctx, worker, consumer, 1, time.Second)
		close(done)
	}()
	<-worker.started
	cancel()
	select {
	case <-activeCancelled:
	case <-time.After(time.Second):
		t.Fatal("active runs were not cancelled during shutdown")
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
