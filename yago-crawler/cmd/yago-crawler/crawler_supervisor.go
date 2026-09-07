package main

import (
	"context"
	"log/slog"
	"time"
)

type crawlWorker interface {
	RunWorkers(acceptCtx, fetchCtx context.Context, workers int)
}

type orderConsumer interface {
	Run(ctx context.Context)
	SuspendActiveRuns()
	WaitForSettlements()
}

func superviseCrawlWithConcurrency(
	ctx context.Context,
	worker crawlWorker,
	consumer orderConsumer,
	workerConcurrency *workerConcurrency,
	grace time.Duration,
) {
	fetchCtx, cancelFetch := context.WithCancel(context.Background())
	defer cancelFetch()

	workersDone := make(chan struct{})
	go func() {
		runResizableWorkers(ctx, fetchCtx, worker, workerConcurrency)
		close(workersDone)
	}()
	consumerDone := make(chan struct{})
	go func() {
		consumer.Run(ctx)
		close(consumerDone)
	}()

	<-consumerDone
	select {
	case <-workersDone:
	case <-time.After(grace):
		slog.WarnContext(ctx, "crawler shutdown grace elapsed, aborting in-flight fetches",
			slog.Duration("grace", grace))
		cancelFetch()
		<-workersDone
	}
	consumer.SuspendActiveRuns()
	consumer.WaitForSettlements()
}

func runResizableWorkers(
	ctx context.Context,
	fetchCtx context.Context,
	worker crawlWorker,
	workerConcurrency *workerConcurrency,
) {
	for ctx.Err() == nil {
		workerConcurrency.DrainChanges()
		acceptCtx, cancelAccept := context.WithCancel(ctx)
		runDone := make(chan struct{})
		go func(workers int) {
			worker.RunWorkers(acceptCtx, fetchCtx, workers)
			close(runDone)
		}(workerConcurrency.Current())

		select {
		case <-ctx.Done():
			cancelAccept()
			<-runDone

			return
		case <-workerConcurrency.Changes():
			cancelAccept()
			<-runDone
		}
	}
}
