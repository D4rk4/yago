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

type fakeOrderConsumer struct{}

func (fakeOrderConsumer) Run(ctx context.Context) { <-ctx.Done() }

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
