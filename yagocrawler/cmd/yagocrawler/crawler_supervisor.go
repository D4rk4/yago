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
}

// superviseCrawl runs the workers and the order consumer until ctx is cancelled,
// then stops accepting new work and lets in-flight fetches finish within grace
// before aborting them. Workers pull new jobs under ctx but fetch under a
// separate context, so cancelling ctx halts intake without dropping current work.
func superviseCrawl(
	ctx context.Context,
	worker crawlWorker,
	consumer orderConsumer,
	workers int,
	grace time.Duration,
) {
	fetchCtx, cancelFetch := context.WithCancel(context.Background())
	defer cancelFetch()

	workersDone := make(chan struct{})
	go func() {
		worker.RunWorkers(ctx, fetchCtx, workers)
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
}
